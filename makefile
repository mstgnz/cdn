include .env

.PHONY: build up up_build down clean
.DEFAULT_GOAL:= up

build:
	docker-compose build

up:
	docker-compose up -d

up_build:
	docker-compose down
	docker-compose up --build -d

down:
	docker-compose down

clean:
	docker rmi $(docker images -f "dangling=true" -q)

run_api:
	docker run -d --restart=always -p 8080:9090 --name cdn-golang --network=cdn_cdn cdn-golang bash -c "go build -o CdnApp && ./CdnApp"

run_minio:
	docker run -d --restart always -p 9000:9000 -p 9001:9001 --name cdn-minio --volume=minio:/var/lib/minio -e MINIO_ROOT_USER='${MINIO_ROOT_USER}' -e MINIO_ROOT_PASSWORD='${MINIO_ROOT_PASSWORD}' minio/minio server --console-address ":9001" /var/lib/minio