SERVICE = goversionbot
PROJ := XXXXX 
TAG = gcr.io/$(PROJ)/$(SERVICE)

build: 
	CGO_ENABLED=0 GOOS=linux go build -o ./app/server ./app 

docker-running:
	@until docker info &> /dev/null; do echo 'waiting for docker...'; sleep 1; done && echo "docker started"

deploy: docker-running build
	docker build --tag $(TAG) ./app/. 
	docker push $(TAG)
	gcloud run deploy $(SERVICE) --image=$(TAG) --project=$(PROJ) --platform=managed --max-instances=1

.PHONY: build deploy
