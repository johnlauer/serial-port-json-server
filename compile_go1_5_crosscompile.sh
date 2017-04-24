# git submodule init
# git submodule update

echo "About to cross-compile Serial Port JSON Server with Go 1.5"
if [ "$1" = "" ]; then
        echo "You need to pass in the version number as the first parameter like ./compile_go1_5_crosscompile 1.87"
        exit
fi

rm -rf snapshot/*

cp README.md snapshot/

echo "Building Linux amd64"
mkdir snapshot/serial-port-json-server-$1_linux_amd64
mkdir snapshot/serial-port-json-server-$1_linux_amd64/arduino
cp sample* snapshot/serial-port-json-server-$1_linux_amd64
cp -r arduino/hardware  snapshot/serial-port-json-server-$1_linux_amd64/arduino/hardware
cp -r arduino/tools_linux_64  snapshot/serial-port-json-server-$1_linux_amd64/arduino/tools
env GOOS=linux GOARCH=amd64 go build -v -o snapshot/serial-port-json-server-$1_linux_amd64/serial-port-json-server
cd snapshot
tar -zcvf serial-port-json-server-$1_linux_amd64.tar.gz serial-port-json-server-$1_linux_amd64
cd ..

echo "" 
echo "Building Linux 386"
mkdir snapshot/serial-port-json-server-$1_linux_386
mkdir snapshot/serial-port-json-server-$1_linux_386/arduino
cp sample* snapshot/serial-port-json-server-$1_linux_386
cp -r arduino/hardware  snapshot/serial-port-json-server-$1_linux_386/arduino/hardware
cp -r arduino/tools_linux_32  snapshot/serial-port-json-server-$1_linux_386/arduino/tools
env GOOS=linux GOARCH=386 go build -v -o snapshot/serial-port-json-server-$1_linux_386/serial-port-json-server
cd snapshot
tar -zcvf serial-port-json-server-$1_linux_386.tar.gz serial-port-json-server-$1_linux_386
cd ..

echo "" 
echo "Building Linux ARM (Raspi)"
mkdir snapshot/serial-port-json-server-$1_linux_arm
mkdir snapshot/serial-port-json-server-$1_linux_arm/arduino
cp sample* snapshot/serial-port-json-server-$1_linux_arm
cp -r arduino/hardware  snapshot/serial-port-json-server-$1_linux_arm/arduino/hardware
cp -r arduino/tools_linux_arm  snapshot/serial-port-json-server-$1_linux_arm/arduino/tools
env GOOS=linux GOARCH=arm go build -v -o snapshot/serial-port-json-server-$1_linux_arm/serial-port-json-server
cd snapshot
tar -zcvf serial-port-json-server-$1_linux_arm.tar.gz serial-port-json-server-$1_linux_arm
cd ..

echo "" 
echo "Building Windows x32"
mkdir snapshot/serial-port-json-server-$1_windows_386
mkdir snapshot/serial-port-json-server-$1_windows_386/arduino
mkdir snapshot/serial-port-json-server-$1_windows_386/drivers
cp -r drivers/* snapshot/serial-port-json-server-$1_windows_386/drivers
cp sample* snapshot/serial-port-json-server-$1_windows_386
cp -r arduino/hardware  snapshot/serial-port-json-server-$1_windows_386/arduino/hardware
cp -r arduino/tools_windows  snapshot/serial-port-json-server-$1_windows_386/arduino/tools
env GOOS=windows GOARCH=386 go build -v -o snapshot/serial-port-json-server-$1_windows_386/serial-port-json-server.exe
cd snapshot/serial-port-json-server-$1_windows_386
zip -r ../serial-port-json-server-$1_windows_386.zip *
cd ../..

echo "" 
echo "Building Windows x64"
mkdir snapshot/serial-port-json-server-$1_windows_amd64
mkdir snapshot/serial-port-json-server-$1_windows_amd64/arduino
mkdir snapshot/serial-port-json-server-$1_windows_amd64/drivers
cp -r drivers/* snapshot/serial-port-json-server-$1_windows_amd64/drivers
cp sample* snapshot/serial-port-json-server-$1_windows_amd64
cp -r arduino/hardware  snapshot/serial-port-json-server-$1_windows_amd64/arduino/hardware
cp -r arduino/tools_windows  snapshot/serial-port-json-server-$1_windows_amd64/arduino/tools
env GOOS=windows GOARCH=amd64 go build -v -o snapshot/serial-port-json-server-$1_windows_amd64/serial-port-json-server.exe
cd snapshot/serial-port-json-server-$1_windows_amd64
zip -r ../serial-port-json-server-$1_windows_amd64.zip *
cd ../..

echo "" 
echo "Building Darwin x64"
mkdir snapshot/serial-port-json-server-$1_darwin_amd64
mkdir snapshot/serial-port-json-server-$1_darwin_amd64/arduino
cp sample* snapshot/serial-port-json-server-$1_darwin_amd64
cp -r arduino/hardware  snapshot/serial-port-json-server-$1_darwin_amd64/arduino/hardware
cp -r arduino/tools_darwin  snapshot/serial-port-json-server-$1_darwin_amd64/arduino/tools
env GOOS=darwin GOARCH=amd64 go build -v -o snapshot/serial-port-json-server-$1_darwin_amd64/serial-port-json-server
cd snapshot/serial-port-json-server-$1_darwin_amd64
zip -r ../serial-port-json-server-$1_darwin_amd64.zip *
cd ../..

export GITHUB_TOKEN=d1ce644ff5eef10f4f1e5fbf27515d22c7d68e8b
