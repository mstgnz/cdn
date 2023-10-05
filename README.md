## CDN API with Go and MinIO
#### Create your own Cdn service on Minio and Aws with Golang.

### Overview

This project allows you to create your own Content Delivery Network (CDN) service using MinIO and AWS S3 with the Go programming language. You can use this CDN service to upload, retrieve, and delete images.

### Prerequisites
Before you get started, make sure you have the following prerequisites installed on your computer:
* [Docker](https://www.docker.com/): You will need Docker to run this project.


### Installation

Follow these steps to set up and run the project:

1- Clone the repository:
```bash
git clone https://github.com/mstgnz/go-minio-cdn.git
```
2- Rename the .env.example file to .env and enter the required information.
3- Start the project with Docker Compose:
```bash
docker-compose up -d
```
Now, you can access the following services:
* MinIO: http://localhost:9001
* Go API: http://localhost:9090

### Postman Collection
You can find a [Postman Collection](go-minio-cdn.postman_collection.json) for this project in the go-minio-cdn.postman_collection.json file.


### Image Upload

#### Upload to MinIO

* HTTP POST: http://localhost:9090/upload
* Headers:
  * Authorization (from .env)
* Body (form-data):

| KEY    | VALUE       |
|--------|-------------|
| bucket | bucket name |
| path   | slider      |
| file   | choose file |


#### Upload to MinIO and AWS S3

* HTTP POST: http://localhost:9090/upload-with-aws
* Headers:
    * Authorization (from .env)
* Body (form-data):

| KEY    | VALUE       |
|--------|-------------|
| bucket | bucket name |
| path   | slider      |
| file   | choose file |


### Image Get

#### Get Image
* HTTP GET: http://localhost:9090/bucket-name/object-name

#### Get Image with Custom Width and Height
* HTTP GET: http://localhost:9090/bucketname/300/250/object-name

### Image Delete

#### Delete from MinIO
* HTTP DELETE: http://localhost:9090/delete
* Headers:
  * Authorization (from .env)
* Body (form-data):

| KEY    | VALUE       |
|--------|-------------|
| bucket | bucket name |
| object | object name |


#### Delete from MinIO and AWS S3
* HTTP DELETE: http://localhost:9090/delete-with-aws
* Headers:
    * Authorization (from .env)
* Body (form-data):

| KEY    | VALUE       |
|--------|-------------|
| bucket | bucket name |
| object | object name |

### Contributing
This project is open-source, and contributions are welcome. Feel free to contribute or provide feedback of any kind.


### License
This project is licensed under the Apache License. See the [LICENSE](https://github.com/mstgnz/go-minio-cdn/blob/main/LICENSE) file for more details.