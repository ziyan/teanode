# Build the dashboard, then the server with the dashboard compiled into it
# and the client beside it. The runtime includes local media readers, with no
# shell or package manager.

FROM node:22 AS dashboard
WORKDIR /src
# Install dependencies first so that a source-only change does not repeat it.
COPY web/package.json web/package-lock.json ./web/
COPY web/scripts/patch-braces.cjs ./web/scripts/
RUN cd web && npm ci --no-audit --no-fund
COPY web ./web
# The build writes into internal/frontend/static, which the Go build embeds.
RUN mkdir -p internal/frontend/static && cd web && npm run build

FROM golang:1.26.10 AS server
# Passed by the release workflow so the binary can report what it is. Without
# them "teanode-server version" says 0.0.0-dev, which is useless in a bug
# report.
ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
WORKDIR /src
COPY . .
COPY --from=dashboard /src/internal/frontend/static ./internal/frontend/static
RUN CGO_ENABLED=0 go generate -mod=vendor ./internal/util/commentparse \
 && CGO_ENABLED=0 go build -mod=vendor \
    -ldflags "-s -w -extldflags \"-static\" \
      -X github.com/ziyan/teanode/internal/version.version=${VERSION} \
      -X github.com/ziyan/teanode/internal/version.commit=${COMMIT}" \
    -o /teanode-server ./cmd/teanode-server \
 && CGO_ENABLED=0 go build -mod=vendor \
    -ldflags "-s -w -extldflags \"-static\" \
      -X github.com/ziyan/teanode/internal/version.version=${VERSION} \
      -X github.com/ziyan/teanode/internal/version.commit=${COMMIT}" \
    -o /teanode ./cmd/teanode

# poppler reads the text out of an uploaded PDF and renders a scanned one
# as pictures. poppler-data holds the character maps a PDF in Chinese,
# Japanese or Korean names its fonts by; without them that text reads empty.
# The fontconfig files and the DejaVu fonts it installs with are what
# pdftoppm draws a font the PDF does not carry with.
#
# libheif's heif-dec turns a photo from an iPhone (HEIC, stored as tiles and
# turned by a flag) into a JPEG; ffmpeg decodes only one tile of it. libheif
# loads its HEVC decoder as a plugin when it runs, so the plugin directory and
# the libraries the plugins link are copied too.
FROM debian:trixie-slim AS media-readers
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg poppler-utils poppler-data libheif-examples libheif-plugin-libde265 \
 && mkdir -p /reader-root/usr/local/bin /reader-root/usr/share/doc \
 && cp /usr/bin/ffmpeg /usr/bin/ffprobe /usr/bin/pdftotext /usr/bin/pdftoppm /usr/bin/heif-dec /reader-root/usr/local/bin/ \
 && cp -a --parents /usr/share/poppler /etc/fonts /usr/share/fonts /usr/share/fontconfig /reader-root \
 && heif_plugins="$(dpkg -L libheif-plugin-libde265 | grep -E '/libheif/plugins/.*[.]so$' | head -n 1 | xargs dirname)" \
 && test -n "${heif_plugins}" \
 && cp -a --parents "${heif_plugins}" /reader-root \
 && ldd /usr/bin/ffmpeg /usr/bin/ffprobe /usr/bin/pdftotext /usr/bin/pdftoppm /usr/bin/heif-dec "${heif_plugins}"/*.so \
    | awk '/=> \// {print $3} $1 ~ /^\// && $2 ~ /^\(/ {print $1}' \
    | sed -e 's@^/lib/@/usr/lib/@' -e 's@^/lib64/@/usr/lib64/@' \
    | sort -u | xargs -r cp -L --parents -t /reader-root \
 && cp -a /usr/share/doc/. /reader-root/usr/share/doc/ \
 && rm -rf /var/lib/apt/lists/*

# The face reader: a small C++ program on OpenCV's detector and recognizer,
# and the two models it needs. Faces are found by a separate program so that
# the server stays free of C code. Nothing built here but the program, the
# libraries it links at run time and the models goes into the runtime image:
# not the compiler and not the -dev packages.
#   YuNet (face_detection_yunet_2023mar.onnx), MIT license, finds the faces.
#   SFace (face_recognition_sface_2021dec.onnx), Apache 2.0, describes them.
FROM debian:trixie-slim AS faces-reader
RUN apt-get update \
 && apt-get install -y --no-install-recommends g++ libopencv-objdetect-dev libopencv-dnn-dev libopencv-imgproc-dev libopencv-core-dev curl ca-certificates \
 && rm -rf /var/lib/apt/lists/*
COPY tools/faces/teanode-faces.cpp /src/teanode-faces.cpp
RUN mkdir -p /faces-root/usr/local/bin /faces-root/usr/share/teanode/faces \
 && g++ -O2 -std=c++17 /src/teanode-faces.cpp -o /faces-root/usr/local/bin/teanode-faces \
    -I/usr/include/opencv4 -lopencv_objdetect -lopencv_dnn -lopencv_imgproc -lopencv_core \
 && cd /faces-root/usr/share/teanode/faces \
 && curl --fail --silent --show-error --location -O https://github.com/opencv/opencv_zoo/raw/main/models/face_detection_yunet/face_detection_yunet_2023mar.onnx \
 && curl --fail --silent --show-error --location -O https://github.com/opencv/opencv_zoo/raw/main/models/face_recognition_sface/face_recognition_sface_2021dec.onnx \
 && echo "8f2383e4dd3cfbb4553ea8718107fc0423210dc964f9f4280604804ed2552fa4  face_detection_yunet_2023mar.onnx" | sha256sum -c - \
 && echo "0ba9fbfa01b5270c96627c4ef784da859931e02f04419c829e83484087c34e79  face_recognition_sface_2021dec.onnx" | sha256sum -c - \
 && ldd /faces-root/usr/local/bin/teanode-faces \
    | awk '/=> \// {print $3} $1 ~ /^\// && $2 ~ /^\(/ {print $1}' \
    | sed -e 's@^/lib/@/usr/lib/@' -e 's@^/lib64/@/usr/lib64/@' \
    | sort -u | xargs -r cp -L --parents -t /faces-root

# The readers and their linked libraries come from the same distribution as
# the runtime. Only descriptors and pipe input are admitted by the decoder.
FROM gcr.io/distroless/base-debian13:nonroot AS runtime
COPY --from=media-readers /reader-root/ /
COPY --from=faces-reader /faces-root/ /

FROM server AS recording-test-build
RUN CGO_ENABLED=0 go test -mod=vendor -c -o /recording-tests ./internal/recording

# The image check exercises the packaged readers without networking. Decoder
# tests may skip on developer machines, but must never skip in this target.
FROM runtime AS recording-tests
COPY --from=recording-test-build /recording-tests /usr/local/bin/recording-tests
ENV TEANODE_RECORDING_DECODER_REQUIRED=1
ENTRYPOINT ["/usr/local/bin/recording-tests"]
CMD ["-test.v", "-test.run", "Audio|PrivateMedia"]

# The :nonroot tag runs as uid 65532 rather than root. Binding 25, 80, 443 and
# 587 still needs CAP_NET_BIND_SERVICE, which the compose files grant — the
# capability is the whole of what root was being used for, and granting just
# that is smaller than granting all of it.
FROM runtime AS production
COPY --from=server /teanode-server /usr/local/bin/teanode-server
# The client too, so that "docker compose exec teanode teanode user list"
# administers the server from inside its own container with nothing set up.
COPY --from=server /teanode /usr/local/bin/teanode
# Mail, the dashboard, and the ACME http-01 challenge.
EXPOSE 25 80 443 587
ENTRYPOINT ["/usr/local/bin/teanode-server"]
CMD ["run"]
