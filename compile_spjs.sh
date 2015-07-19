# git submodule init
# git submodule update

echo "About to cross-compile Serial Port JSON Server"
if [ "$1" = "" ]; then
        echo "You need to pass in the version number as the first parameter."
        exit
fi

cp -r arduino/tools_linux_64  arduino/tools
goxc -os="linux" -arch="amd64" --include="arduino/hardware,arduino/tools" -n="serial-port-json-server_$1" -d=.
rm -rf arduino/tools

cp -r arduino/tools_linux_32  arduino/tools
goxc -os="linux" -arch="386" --include="arduino/hardware,arduino/tools" -n="serial-port-json-server_$1" -d=.
rm -rf arduino/tools

cp -r arduino/tools_linux_arm  arduino/tools
goxc -os="linux" -arch="arm" --include="arduino/hardware,arduino/tools" -n="serial-port-json-server_$1" -d=.
rm -rf arduino/tools

cp -r arduino/tools_windows  arduino/tools
goxc -os="windows" --include="arduino/hardware,arduino/tools" -n="serial-port-json-server_$1" -d=.
rm -rf arduino/tools

cp -r arduino/tools_darwin  arduino/tools
goxc -os="darwin" --include="arduino/hardware,arduino/tools" -n="serial-port-json-server_$1" -d=.
rm -rf arduino/tools

sudo mkdir "/media/sf_downloads/v$1"
sudo cp snapshot/*.zip snapshot/*.gz snapshot/*.md snapshot/*.deb "/media/sf_downloads/v$1/"