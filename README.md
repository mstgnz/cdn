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
git clone https://github.com/mstgnz/cdn.git
```
2- Rename the .env.example file to .env and enter the required information.
3- Start the project with Docker Compose:
```bash
docker-compose up -d
```
Now, you can access the following services:
* MinIO: http://localhost:9001
* Go API: http://localhost:9090


### Usage
- All usage: From the [Swagger UI](http://localhost:9090/swagger) user interface you can see and try all the uses. Or you can review [swagger.yaml](/public/swagger.yaml).
- You can submit width and height if you want it to be resized during upload. If you send only width, the height will be assigned proportionally. If you send only the height, the width will be assigned proportionally. The resizing process is optional.
- Get: if you use only width, height = will be proportioned according to the original size. If you use only height, width = will be proportioned according to the original size.


### Contributing
This project is open-source, and contributions are welcome. Feel free to contribute or provide feedback of any kind.


### License
This project is licensed under the Apache License. See the [LICENSE](LICENSE) file for more details.