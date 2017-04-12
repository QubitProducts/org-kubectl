.PHONY: bootstrap
bootstrap:
	go get github.com/Masterminds/glide
	glide i -v

.PHONY: build
build:
	go build -o org-kubctl .
