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

build_api:
	docker build -t cdn-golang .

run_api: build_api create_volume create_network
	docker run -d \
		--restart=always \
		--name cdn-golang \
		--log-driver none \
		--network=$(APP_NAME) \
		--env-file .env \
		-p $(APP_PORT):$(APP_PORT) \
		cdn-golang bash -c "go build -o CdnApp && ./CdnApp"

run_minio: create_network
	docker run -d \
		-p 9000:9000 \
		-p 9001:9001 \
		--name cdn-minio \
		--restart always \
		--network=$(APP_NAME) \
		--volume=minio:/var/lib/minio \
		-e MINIO_ROOT_USER='$(MINIO_ROOT_USER)' \
		-e MINIO_ROOT_PASSWORD='$(MINIO_ROOT_PASSWORD)' \
		minio/minio server --console-address ":9001" /var/lib/minio

create_network:
	@if ! docker network inspect $(APP_NAME) >/dev/null 2>&1; then \
		docker network create $(APP_NAME); \
	else \
		echo "Network '$(APP_NAME)' already exists, using existing network."; \
	fi

create_volume:
	@if ! docker volume inspect $(APP_NAME) >/dev/null 2>&1; then \
		docker volume create $(APP_NAME); \
	else \
		echo "Volume '$(APP_NAME)' already exists, skipping creation."; \
	fi
