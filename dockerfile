FROM golang:1.22-bullseye

# Set environment variables
ENV DEBIAN_FRONTEND=noninteractive

# Install dependencies and ImageMagick
RUN apt-get update && apt-get install -y \
    git \
    gcc \
    curl \
    wget \
    build-essential \
    pkg-config \
    libpng-dev \
    libjpeg-dev \
    libtiff-dev \
    libwebp-dev \
    libmagickwand-dev \
    libmagickcore-dev \
    imagemagick \
    && ldconfig

# Copy and run version check script
COPY scripts/get_imagemagick_version.sh /tmp/
RUN chmod +x /tmp/get_imagemagick_version.sh && \
    cd /tmp && \
    VERSION=$(/tmp/get_imagemagick_version.sh) && \
    wget https://download.imagemagick.org/archive/releases/ImageMagick-${VERSION}.tar.gz && \
    tar xvzf ImageMagick-${VERSION}.tar.gz && \
    cd ImageMagick-* && \
    ./configure && \
    make && \
    make install && \
    ldconfig /usr/local/lib && \
    cd / && \
    apt-get remove -y wget && \
    apt-get autoremove -y && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* && \
    rm -rf /tmp/*

# Install the hardened ImageMagick security policy. MAGICK_CONFIGURE_PATH makes
# it authoritative regardless of the ImageMagick major version or install prefix
# (source-built IM7 under /usr/local vs apt IM6 under /etc), closing the
# Ghostscript-delegate / ImageTragick RCE classes on untrusted uploads.
COPY docker/policy.xml /etc/ImageMagick/policy.xml
ENV MAGICK_CONFIGURE_PATH=/etc/ImageMagick

WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=1 GOOS=linux go build -o main ./cmd/main.go

EXPOSE 9090
ENTRYPOINT ["./main"]
