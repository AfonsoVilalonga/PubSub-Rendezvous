package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
	"sync"

	"cloud.google.com/go/pubsub"
	ecies "github.com/ecies/go/v2"
	"github.com/google/uuid"
)

const (
	CONNECT    = 1
	PUBLIC_KEY = 4
	DATA       = 9
	SYM        = 2
	HAS_BRIDGE = 5
	DONE       = 10
)

var (
	projectID           string
	topicDefault        string
	credentialFile      string
	subscriptionDefault string
	valueID             string

	csRendezvousConn         net.Conn
	clientConnectedCtx       context.Context
	clientConnectedCtxCancel context.CancelFunc

	pkMux sync.Mutex

	publicKey *ecies.PublicKey
	symKey    []byte
)

func packetize(msg []byte) []byte {
	data_len := make([]byte, 4)
	valueToAdd := (1024 - ((len(msg) + 4) % 1024)) % 1024
	padding := make([]byte, valueToAdd)

	binary.BigEndian.PutUint32(data_len, uint32(len(msg)))
	combinedBuffer := append(msg, padding...)
	combinedBuffer = append(data_len, combinedBuffer...)

	return combinedBuffer
}

func decryptMsg(msg []byte, key []byte) []byte {
	aes, err := aes.NewCipher([]byte(key))
	if err != nil {
		return nil
	}

	gcm, err := cipher.NewGCM(aes)
	if err != nil {
		return nil
	}

	nonceSize := gcm.NonceSize()
	nonce, ciphertext := msg[:nonceSize], msg[nonceSize:]

	plaintext, err := gcm.Open(nil, []byte(nonce), []byte(ciphertext), nil)
	if err != nil {
		fmt.Println(err)
		return nil
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

func generateAESKey() {
	if _, err := rand.Read(symKey); err != nil {
		fmt.Println("Error generating random key:", err)
	}
}

func handleMessages(ctx context.Context, client *pubsub.Client, topic *pubsub.Topic) {
	sub := client.Subscription(subscriptionDefault)

	sub.Receive(ctx, func(cctx context.Context, msg *pubsub.Message) {
		msg.Ack()
		if msg.Data[0] == PUBLIC_KEY && publicKey == nil {
			pkMux.Lock()
			if publicKey == nil {
				auxPK := msg.Data[1:]
				publicKey, _ = ecies.NewPublicKeyFromBytes(auxPK)

				sendMsg, _ := ecies.Encrypt(publicKey, symKey)
				sendMsg = append([]byte(valueID), sendMsg...)
				sendMsg = append([]byte{SYM}, sendMsg...)
				sendMessage(ctx, topic, sendMsg)
			}
			pkMux.Unlock()
		} else if msg.Data[0] == HAS_BRIDGE {
			aux := decryptMsg(msg.Data[1:], symKey)
			if aux != nil {
				msg_id := aux[:36]
				if string(msg_id) == valueID {
					csRendezvousConn.Write(packetize([]byte{HAS_BRIDGE}))
				}
			}
		} else if msg.Data[0] == DATA {
			aux := decryptMsg(msg.Data[1:], symKey)
			if aux != nil {
				msg_id := aux[:36]
				if string(msg_id) == valueID {
					csRendezvousConn.Write(packetize(aux[36:]))
				}
			}
		}
	})
}

func getFullPacket() []byte {
	buffer := make([]byte, 1024)
	csRendezvousConn.Read(buffer)

	lenMessage := binary.BigEndian.Uint32([]byte{buffer[0], buffer[1], buffer[2], buffer[3]})
	numberOfPackets := ((((1024 - ((lenMessage + 4) % 1024)) % 1024) + (lenMessage + 4)) / 1024) - 1

	for {
		if numberOfPackets <= 0 {
			break
		}

		var payload_aux []byte = make([]byte, 1024)
		csRendezvousConn.Read(payload_aux)

		buffer = append(buffer, payload_aux...)

		numberOfPackets = numberOfPackets - 1
	}

	buffer = buffer[4 : lenMessage+4]

	return buffer
}

func sendMessage(ctx context.Context, send_topic *pubsub.Topic, msg []byte) {
	result := send_topic.Publish(ctx, &pubsub.Message{
		Data: msg,
	})

	result.Get(ctx)
}

func initializeServer(ctx context.Context, topic_send *pubsub.Topic) {
	listener, err := net.Listen("tcp", "localhost:8009")
	if err != nil {
		fmt.Println("Error listening:", err)
		return
	}
	defer listener.Close()

	conn, err := listener.Accept()
	if err != nil {
		fmt.Println("Error accepting connection:", err)
	}

	clientConnectedCtxCancel()
	csRendezvousConn = conn

	for {
		msg := getFullPacket()
		msg = encryptMsg(msg, symKey)
		msg = append([]byte(valueID), msg...)
		msg = append([]byte{DATA}, msg...)

		sendMessage(ctx, topic_send, msg)
	}
}

func main() {
	credentialFile = "CREDENTIAL_FILE"
	os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", credentialFile)

	valueID = uuid.New().String()
	projectID = "PROJECT_ID"
	topicDefault = "USER_TOPIC"

	symKey = make([]byte, 32)
	generateAESKey()
	publicKey = nil

	ctx := context.Background()

	clientConnectedCtx, clientConnectedCtxCancel = context.WithCancel(context.Background())

	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("Error creating Pub/Sub client: %v", err)
	}

	sendTopic := client.Topic(topicDefault)

	subSettings := pubsub.SubscriptionConfig{
		Topic: client.Topic("BROKER_TOPIC"),
	}

	subscriptionDefault = "x" + valueID
	_, err = client.CreateSubscription(ctx, subscriptionDefault, subSettings)
	if err != nil {
		fmt.Println("Failed to create subscription:", err)
		return
	}

	go handleMessages(ctx, client, sendTopic)
	go initializeServer(ctx, sendTopic)

	<-clientConnectedCtx.Done()
	msg := []byte(valueID)
	msg = append([]byte{CONNECT}, msg...)
	sendMessage(ctx, sendTopic, msg)

	select {}

}
