## Pub/Sub Rendezvous Protocol 

This repository holds the code for the paper "Looking at the Clouds: Leveraging Pub/Sub Cloud Services for Censorship-Resistant Rendezvous Channels". If you end up using our code for your experiments, please cite our work as follows:

```
@inproceedings{Vilalonga2024a,
	author = {Afonso Vilalonga and João S. Resende and Henrique Domingos},
	title = {Looking at the Clouds: Leveraging Pub/Sub Cloud Services for Censorship-Resistant Rendezvous Channels},
	booktitle = {Free and Open Communications on the Internet},
	publisher = {},
	year = {2024},
	url = {https://www.petsymposium.org/foci/2024/foci-2024-0010.pdf},
}
```

## Configuration
To test our system, you need a free Google Cloud Platform account and some preliminary configurations. Specifically:
* A project.
* A service account and its respective key (more details can be found in [service accounts](https://cloud.google.com/iam/docs/service-account-overview) and [keys](https://cloud.google.com/iam/docs/service-account-creds#key-types)).
* The Pub/Sub service activated.
* A topic created for the Broker in the Pub/Sub service (i.e., BROKER_TOPIC or BT).
* A topic created for the Users in the Pub/Sub service (i.e., USER_TOPIC or UT).
* A subscription to the BROKER_TOPIC (i.e., BROKER_SUB).

All of this can be easily set up by following the Google Cloud Platform tutorials.

Then, the respective names must be configured in our system. Specifically:

For the Server (i.e., Broker) (server.go):
* The PROJECT_ID must be the project ID of the created project.
* The BROKER_SUB must be the name of the already created subscription for the BROKER_TOPIC.
* The BROKER_TOPIC must be the name of the topic created for the Broker.
* The CREDENTIAL_FILE is the location of the file with the key for the service account created.

For the Users (client.go):
* The PROJECT_ID must be the project ID of the created project.
* The USER_TOPIC must be the name of the topic created for the Users.
* The BROKER_TOPIC must be the name of the topic created for the Broker.
* The CREDENTIAL_FILE is the location of the file with the key for the service account created.

The system was developed to be tested with [TorKameleon](https://arxiv.org/abs/2303.17544). To test it with other systems, changes might need to be made to the code; however, the main protocol can theoretically remain the same. The architecture of the system is described in the paper, and any questions or suggestions can be made via issues on GitHub or to my email available in the paper.

## TODO
* Review the protocol and simplify it.
* Add a configuration file for the system.
