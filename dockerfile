FROM golang:1.20 as builder

# Ignore APT warnings about not having a TTY
ENV DEBIAN_FRONTEND=noninteractive \
    IMAGEMAGICK_VERSION=7.1.1-18

RUN apt-get update \
    && apt-get install -y \
        wget build-essential \
        ffmpeg \
        pkg-config \
        libmagickcore-dev libmagickwand-dev \
        libjpeg-dev \
        libpng-dev \
        libtiff-dev \
        libgif-dev \
        libx11-dev \
        fontconfig fontconfig-config libfontconfig1-dev \
        ghostscript gsfonts gsfonts-x11 \
        libfreetype6-dev \
        vim \
        --no-install-recommends \
    && rm -rf /var/lib/apt/lists/*

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

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN GOOS=linux go build -o CdnApp ./cmd
ENTRYPOINT ["/app/CdnApp"]