FROM golang:1.20 as compiler

# Ignore APT warnings about not having a TTY
ENV DEBIAN_FRONTEND noninteractive

RUN apt-get update \
    && apt-get install -y \
        wget build-essential \
        ffmpeg \
        pkg-config \
        --no-install-recommends \
    && apt-get -q -y install vim \
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

ENV IMAGEMAGICK_VERSION=7.1.1-15

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

WORKDIR /go-minio-cdn
COPY go.mod go.sum ./
RUN go mod download
COPY . .
#RUN go build -o CdnApp .
#CMD ["./CdnApp"]
RUN curl -sSfL https://raw.githubusercontent.com/cosmtrek/air/master/install.sh | sh -s -- -b $(go env GOPATH)/bin
CMD ["air"]