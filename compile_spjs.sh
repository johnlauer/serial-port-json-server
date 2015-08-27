# git submodule init
# git submodule update

echo "About to cross-compile Serial Port JSON Server"
if [ "$1" = "" ]; then
        echo "You need to pass in the version number as the first parameter."
        exit
fi

rm -rf snapshot/*

cp README.md snapshot/

cp -r arduino/tools_linux_64  arduino/tools
goxc -os="linux" -arch="amd64" --include="arduino/hardware,arduino/tools" -n="serial-port-json-server" -d=.
rm -rf arduino/tools
mv snapshot/serial-port-json-server_linux_amd64.tar.gz snapshot/serial-port-json-server-$1_linux_amd64.tar.gz

cp -r arduino/tools_linux_32  arduino/tools
goxc -os="linux" -arch="386" --include="arduino/hardware,arduino/tools" -n="serial-port-json-server" -d=.
rm -rf arduino/tools
mv snapshot/serial-port-json-server_linux_386.tar.gz snapshot/serial-port-json-server-$1_linux_386.tar.gz

cp -r arduino/tools_linux_arm  arduino/tools
goxc -os="linux" -arch="arm" --include="arduino/hardware,arduino/tools" -n="serial-port-json-server" -d=.
rm -rf arduino/tools
mv snapshot/serial-port-json-server_linux_arm.tar.gz snapshot/serial-port-json-server-$1_linux_arm.tar.gz

cp -r arduino/tools_windows  arduino/tools
goxc -os="windows" --include="arduino/hardware,arduino/tools,drivers/windows" -n="serial-port-json-server" -d=.
rm -rf arduino/tools
mv snapshot/serial-port-json-server_windows_386.zip snapshot/serial-port-json-server-$1_windows_386.zip
mv snapshot/serial-port-json-server_windows_amd64.zip snapshot/serial-port-json-server-$1_windows_amd64.zip

cp -r arduino/tools_darwin  arduino/tools
goxc -os="darwin" --include="arduino/hardware,arduino/tools" -n="serial-port-json-server" -d=.
rm -rf arduino/tools
mv snapshot/serial-port-json-server_darwin_386.zip snapshot/serial-port-json-server-$1_darwin_386.zip
mv snapshot/serial-port-json-server_darwin_amd64.zip snapshot/serial-port-json-server-$1_darwin_amd64.zip

// remove snapshot files
rm snapshot/*snapshot*

sudo mkdir "/media/sf_downloads/v$1"
sudo cp snapshot/*.zip snapshot/*.gz snapshot/*.md snapshot/*.deb "/media/sf_downloads/v$1/"
