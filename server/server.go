package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"sync"

	"cloud.google.com/go/pubsub"
	ecies "github.com/ecies/go/v2"
)

const (
	POLL                = 3
	NO_PEDDING_CLIENTS  = 6
	HAS_PEDDING_CLIENTS = 5

	CONNECT    = 1
	SYM        = 2
	PUBLIC_KEY = 4
	DATA       = 9
)

var (
	projectID           string
	subscriptionDefault string
	topicDefault        string
	credentialFile      string

	newRequests  []*client_conn
	connections  map[string]client_conn
	messageQueue chan []byte

	privateKey *ecies.PrivateKey

	muUsers sync.Mutex
)

type client_conn struct {
	id         string
	proxy_conn net.Conn
	symKey     []byte
}

func generateKeys() {
	k, err := ecies.GenerateKey()
	if err != nil {
		panic(err)
	}

	privateKey = k
}

func decryptMsg(msg []byte, key []byte) []byte {
	aes, err := aes.NewCipher([]byte(key))
	if err != nil {
		panic(err)
	}

	gcm, err := cipher.NewGCM(aes)
	if err != nil {
		panic(err)
	}

	nonceSize := gcm.NonceSize()
	nonce, ciphertext := msg[:nonceSize], msg[nonceSize:]

	plaintext, err := gcm.Open(nil, []byte(nonce), []byte(ciphertext), nil)
	if err != nil {
		panic(err)
	}

	return plaintext
}

func encryptMsg(msg []byte, key []byte) []byte {
	aes, err := aes.NewCipher([]byte(key))
	if err != nil {
		panic(err)
	}

	gcm, err := cipher.NewGCM(aes)
	if err != nil {
		panic(err)
	}

	nonce := make([]byte, gcm.NonceSize())
	_, err = rand.Read(nonce)
	if err != nil {
		panic(err)
	}

	ciphertext := gcm.Seal(nonce, nonce, msg, nil)
	return ciphertext
}

func sendMessage(ctx context.Context, send_topic *pubsub.Topic, msg []byte) {
	result := send_topic.Publish(ctx, &pubsub.Message{
		Data: msg,
	})

	result.Get(ctx)
}

func handleRequests(ctx context.Context, client *pubsub.Client, send_topic *pubsub.Topic) {
	sub := client.Subscription(subscriptionDefault)

	sub.Receive(ctx, func(cctx context.Context, msg *pubsub.Message) {
		result := msg.Data
		msg.Ack()
		if msg.Data[0] == CONNECT {
			c := client_conn{
				proxy_conn: nil,
				symKey:     nil,
			}
			c.id = string(result[1:])
			connections[string(result[1:])] = c
			sendMsg := []byte{PUBLIC_KEY}
			sendMsg = append(sendMsg, privateKey.PublicKey.Bytes(false)...)
			sendMessage(ctx, send_topic, sendMsg)
		} else {
			messageQueue <- msg.Data
		}
	})
}

func handleMessage(msg []byte) {
	type_msg := msg[0]
	id := msg[1:37]
	conn := connections[string(id)]

	if type_msg == SYM {
		aux, _ := ecies.Decrypt(privateKey, msg[37:])
		conn.symKey = aux
		connections[string(id)] = conn
		newRequests = append(newRequests, &conn)
	} else if type_msg == DATA {
		result := decryptMsg(msg[37:], conn.symKey)
		conn.proxy_conn.Write(packetize(result))
	}
}

func packetize(msg []byte) []byte {
	data_len := make([]byte, 4)
	valueToAdd := (1024 - ((len(msg) + 4) % 1024)) % 1024
	padding := make([]byte, valueToAdd)

	binary.BigEndian.PutUint32(data_len, uint32(len(msg)))
	combinedBuffer := append(msg, padding...)
	combinedBuffer = append(data_len, combinedBuffer...)

	return combinedBuffer
}

func getFullPacket(proxyCoon net.Conn) []byte {
	buffer := make([]byte, 1024)
	proxyCoon.Read(buffer)

	lenMessage := binary.BigEndian.Uint32([]byte{buffer[0], buffer[1], buffer[2], buffer[3]})
	numberOfPackets := ((((1024 - ((lenMessage + 4) % 1024)) % 1024) + (lenMessage + 4)) / 1024) - 1

	for {
		if numberOfPackets <= 0 {
			break
		}

		var payload_aux []byte = make([]byte, 1024)
		proxyCoon.Read(payload_aux)

		buffer = append(buffer, payload_aux...)

		numberOfPackets = numberOfPackets - 1
	}

	buffer = buffer[4 : lenMessage+4]

	return buffer
}

func handleBridges(proxy_conn net.Conn, ctx context.Context, send_topic *pubsub.Topic) {
	var cc *client_conn
	for {

		packet := getFullPacket(proxy_conn)

		if packet[0] == POLL {
			muUsers.Lock()
			if len(newRequests) > 0 {
				cc = newRequests[0]
				newRequests = newRequests[1:]

				aux := connections[string(cc.id)]
				aux.proxy_conn = proxy_conn
				connections[string(cc.id)] = aux

				bytes := packetize([]byte{HAS_PEDDING_CLIENTS})
				proxy_conn.Write(bytes)
				msg := encryptMsg([]byte(cc.id), cc.symKey)
				msg = append([]byte{HAS_PEDDING_CLIENTS}, msg...)
				sendMessage(ctx, send_topic, msg)
			} else {
				bytes := packetize([]byte{NO_PEDDING_CLIENTS})
				proxy_conn.Write(bytes)
			}
			muUsers.Unlock()
		} else {
			encMsg := append([]byte(cc.id), packet...)
			encMsg = encryptMsg(encMsg, cc.symKey)

			encMsg = append([]byte{DATA}, encMsg...)

			sendMessage(ctx, send_topic, encMsg)
		}
	}
}

func initializeServer(ctx context.Context, send_topic *pubsub.Topic) {
	listener, err := net.Listen("tcp", "localhost:10000")
	if err != nil {
		fmt.Println("Error listening:", err)
		return
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Error listening:", err)
			return
		}
		go handleBridges(conn, ctx, send_topic)
	}
}

func main() {
	generateKeys()
	projectID = "PROJECT_ID"
	subscriptionDefault = "BROKER_SUB"
	topicDefault = "BROKER_TOPIC"
	credentialFile = "CREDENTIAL_FILE"

	os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", credentialFile)

	ctx := context.Background()
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		fmt.Println(err)
	}

	sendTopic := client.Topic(topicDefault)

	newRequests = make([]*client_conn, 0)
	connections = make(map[string]client_conn)
	messageQueue = make(chan []byte)

	go handleRequests(ctx, client, sendTopic)
	go initializeServer(ctx, sendTopic)

	for msg := range messageQueue {
		go handleMessage(msg)
	}
}
