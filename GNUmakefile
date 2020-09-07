TAG=$(git rev-parse --short HEAD)
IMAGE=quay.io/endocrimes/cert-manager-community-day
TAGGED_IMAGE=$(IMAGE):$(TAG)

.PHONY: build-image
build-image:
	docker build -t $(TAGGED_IMAGE) .

.PHONY: publish-image
publish-image: build-image
	docker push $(TAGGED_IMAGE)
