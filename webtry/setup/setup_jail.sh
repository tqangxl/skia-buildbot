apt-get update
apt-get upgrade

apt-get install -y g++ libfreetype6 libfreetype6-dev libpng12-0 libpng12-dev \
libglu1-mesa-dev mesa-common-dev freeglut3-dev libgif-dev libfontconfig \
libfontconfig-dev git python wget libpoppler-cpp-dev libosmesa6-dev \
ttf-mscorefonts-installer git python

mkdir /skia_build
chmod 777 /skia_build

mkdir /skia_build/bin
mkdir /skia_build/fiddle_main
mkdir /skia_build/scripts
