#!/bin/bash

# ImageMagick releases sayfasından en son tar.gz sürümünü al
LATEST_VERSION=$(curl -s https://download.imagemagick.org/archive/releases/ | \
  grep -o 'ImageMagick-[0-9.-]\+\.tar\.gz' | \
  sort -V | \
  tail -n 1 | \
  sed 's/ImageMagick-\(.*\)\.tar\.gz/\1/')

echo $LATEST_VERSION 