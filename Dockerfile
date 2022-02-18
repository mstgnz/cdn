FROM golang:1.17

# Ignore APT warnings about not having a TTY
ENV DEBIAN_FRONTEND noninteractive

RUN apt-get update \
    && apt-get install -y \
        wget build-essential \
        pkg-config \
        --no-install-recommends \
    && apt-get -q -y install \
        libjpeg-dev \
        libpng-dev \
        libtiff-dev \
        libgif-dev \
        libx11-dev \
        fontconfig fontconfig-config libfontconfig1-dev \
        ghostscript gsfonts gsfonts-x11 \
        libfreetype6-dev \
        libmagickcore-dev libmagickwand-dev \
        --no-install-recommends \
    && rm -rf /var/lib/apt/lists/*

ENV IMAGEMAGICK_VERSION=7.1.0-25

RUN cd && \
	wget https://download.imagemagick.org/ImageMagick/download/releases/ImageMagick-${IMAGEMAGICK_VERSION}.tar.gz && \
	tar xvzf ImageMagick-${IMAGEMAGICK_VERSION}.tar.gz && \
	cd ImageMagick* && \
	./configure \
	    --without-magick-plus-plus \
        --without-perl \
        --disable-openmp \
        --with-gvc=no \
        --with-fontconfig=yes \
        --with-freetype=yes \
        --with-gslib \
        --disable-docs && \
	make -j$(nproc) && make install && \
	ldconfig /usr/local/lib

WORKDIR /go/projects/imagick
COPY . .
RUN go install
