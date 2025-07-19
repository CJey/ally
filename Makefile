APPNAME=ally
TARGET=./release/bin/${APPNAME}
MAINFILE=cmd/${APPNAME}/*.go

.PHONY: proto release
build:
	rm -f ${TARGET}
	MAINFILE=${MAINFILE} ./release/build ${APPNAME} ${TARGET}

env:
	@./release/build env

proto:
	cd proto && rm -f *.pb.go && ./protoc

clean:
	rm -rf bin *.rpm *.deb release/bin release/rpm/*.rpm release/dpkg/*.deb
	cd cmd/demo && rm -f atomic/atomic cache/cache httpd/httpd locker/locker

rpm:
	cd release/rpm && CGO_ENABLED=0 go run . && mv *.rpm ../../

rpm-x86:
	cd release/rpm && CGO_ENABLED=0 go run . --arch x86 && mv *.rpm ../../

rpm-amd64:
	cd release/rpm && CGO_ENABLED=0 go run . --arch amd64 && mv *.rpm ../../

rpm-arm:
	cd release/rpm && CGO_ENABLED=0 go run . --arch arm && mv *.rpm ../../

rpm-arm64:
	cd release/rpm && CGO_ENABLED=0 go run . --arch arm64 && mv *.rpm ../../

deb:
	cd release/dpkg && CGO_ENABLED=0 go run . && mv *.deb ../../

deb-x86:
	cd release/dpkg && CGO_ENABLED=0 go run . --arch x86 && mv *.deb ../../

deb-amd64:
	cd release/dpkg && CGO_ENABLED=0 go run . --arch amd64 && mv *.deb ../../

deb-arm:
	cd release/dpkg && CGO_ENABLED=0 go run . --arch arm && mv *.deb ../../

deb-arm64:
	cd release/dpkg && CGO_ENABLED=0 go run . --arch arm64 && mv *.deb ../../

x86: deb-x86 rpm-x86
amd64: deb-amd64 rpm-amd64
x64: amd64

arm: deb-arm rpm-arm
arm64: deb-arm64 rpm-arm64
