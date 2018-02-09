#!/bin/sh
export GITHUB_TOKEN=d1ce644ff5eef10f4f1e5fbf27515d22c7d68e8b

export GITHUB_USER=chilipeppr

export GITHUB_REPO=serial-port-json-server

echo "About to create a Github release for Serial Port JSON Server"
if [ "$1" = "" ]; then
        echo "You need to pass in the version number as the first parameter like ./release 1.87"
        exit
fi

echo ""
echo "Before creating release"
bin/github-release info

bin/github-release release \
    --tag v$1 \
    --name "Serial Port JSON Server" \
    --description "A server for the Internet of Things. Lets you serve up serial ports to websockets so you can write front-end apps for your IoT devices in the browser." \

echo ""
echo "After creating release"
bin/github-release info

echo ""
echo "Uploading binaries"

# upload a file, for example the OSX/AMD64 binary of my gofinance app
bin/github-release upload \
    --tag v$1 \
    --name "serial-port-json-server-$1_linux_amd64.tar.gz" \
    --file snapshot/serial-port-json-server-$1_linux_amd64.tar.gz
bin/github-release upload \
    --tag v$1 \
    --name "serial-port-json-server-$1_linux_386.tar.gz" \
    --file snapshot/serial-port-json-server-$1_linux_386.tar.gz
bin/github-release upload \
    --tag v$1 \
    --name "serial-port-json-server-$1_linux_arm.tar.gz" \
    --file snapshot/serial-port-json-server-$1_linux_arm.tar.gz
bin/github-release upload \
    --tag v$1 \
    --name "serial-port-json-server-$1_windows_386.zip" \
    --file snapshot/serial-port-json-server-$1_windows_386.zip
bin/github-release upload \
    --tag v$1 \
    --name "serial-port-json-server-$1_windows_amd64.zip" \
    --file snapshot/serial-port-json-server-$1_windows_amd64.zip
    
echo ""
echo "Done"