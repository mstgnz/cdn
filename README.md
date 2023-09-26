## Cdn Api With Go and MinIO
#### Create your own Cdn service on Minio and Aws with Golang.

### Installation

Since the project will run on [docker](https://www.docker.com), you must have docker installed on your computer.

You must change the .env.example file name to .env and enter the required information.

- `git clone https://github.com/mstgnz/go-minio-cdn.git`
- `docker-compose up -d`

minio -> http://localhost:9001  
golang -> http://localhost:9090

#### [Postman Collection](go-minio-cdn.postman_collection.json)

### Image Upload

POST: (Minio) http://localhost:9090/upload  
POST: (Minio And Aws S3) http://localhost:9090/upload-with-aws  
WIDTH: Authorization (env.example)

| KEY    | VALUE       |
|--------|-------------|
| bucket | bucket name |
| path   | slider      |
| file   | choose file |


### Image Get

GET IMAGE : http://localhost:9090/bucket-name/object-name  
GET IMAGE WIDTH SIZE : http://localhost:9090/bucketname/300/250/object-name  


### Image Delete
DELETE: (Minio) http://localhost:9090/delete  
DELETE: (Minio And Aws S3) http://localhost:9090/delete-with-aws  
WIDTH: Authorization (env.example)

| KEY    | VALUE       |
|--------|-------------|
| bucket | bucket name |
| object | object name |


### Contributing
This project is open-source, and contributions are welcome. Feel free to contribute or provide feedback of any kind.

### License
This project is licensed under the Apache License. See the [LICENSE](https://github.com/mstgnz/go-minio-cdn/blob/main/LICENSE) file for more details.