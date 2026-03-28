CGO_CFLAGS = -Wno-deprecated-declarations

.PHONY: run build clean

run:
	CGO_CFLAGS="$(CGO_CFLAGS)" go run .

build:
	CGO_CFLAGS="$(CGO_CFLAGS)" go build -o aticatac .

clean:
	rm -f aticatac
