# The driver structure follows qoke/mp-quic-go at 084c13ea3834fbe58cedb63db9208951d714f839 (MIT).
# Ordinary control, R14 smoke and R17's fixed independent matrix share these pins.
FROM golang:1.27.3-bookworm@sha256:d9b6a1beb9474adc2a1ce842573639b44b2a0dd49b6669f51d3821baf9ec37c9


# Refresh only Go; retain the exact OS, native tools and package snapshot.
RUN test "$(dpkg --print-architecture)" = amd64 && \
    curl --fail --silent --show-error --location https://go.dev/dl/go1.27.4.linux-amd64.tar.gz -o /tmp/go1274.tar.gz && \
    printf '%s\n' '7226736c2339719cda28fbf3a54d71aa63bcefe376e98cef00034268dab24840  /tmp/go1274.tar.gz' | sha256sum -c - && \
    rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go1274.tar.gz && \
    rm /tmp/go1274.tar.gz && \
    test "$(GOTOOLCHAIN=local /usr/local/go/bin/go version)" = 'go version go1.27.4 linux/amd64'

# Freeze package resolution as well as the image and both protocol dependencies.
RUN rm -f /etc/apt/sources.list.d/debian.sources && \
    printf '%s\n' \
      'deb [check-valid-until=no] https://snapshot.debian.org/archive/debian/20261031T000000Z bookworm main' \
      'deb [check-valid-until=no] https://snapshot.debian.org/archive/debian/20261031T000000Z bookworm-updates main' \
      'deb [check-valid-until=no] https://snapshot.debian.org/archive/debian-security/20261031T000000Z bookworm-security main' > /etc/apt/sources.list && \
    apt-get update && apt-get install -y --no-install-recommends cmake libssl-dev python3 && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /src/picoquic
COPY interop/multipath/peer.c /src/r17-peer.c
RUN git init -q && git remote add origin https://github.com/private-octopus/picoquic.git && \
    git fetch -q --depth 1 origin fbd46d9a1f54ebf9397f2ee6355b5019ab379fb7 && git checkout -q FETCH_HEAD && \
    test "$(git rev-parse HEAD)" = fbd46d9a1f54ebf9397f2ee6355b5019ab379fb7 && \
    printf '%s\n' 'add_executable(qgf-r17-peer /src/r17-peer.c)' \
      'target_include_directories(qgf-r17-peer PRIVATE ${PTLS_INCLUDE_DIRS})' \
      'target_link_libraries(qgf-r17-peer PRIVATE picoquic-core)' >> CMakeLists.txt && \
    cmake -S . -B build -DCMAKE_BUILD_TYPE=Release -DPICOQUIC_FETCH_PTLS=ON \
      -DPICOQUIC_FETCH_PTLS_TAG=e446233b4100ebcdec60901c05bfb7f4cd369c5d -DBUILD_TESTING=OFF && \
    test "$(git -C build/_deps/picotls-src rev-parse HEAD)" = e446233b4100ebcdec60901c05bfb7f4cd369c5d && \
    cmake --build build --target picoquicdemo qgf-r17-peer -j 4 && \
    cp build/picoquicdemo /usr/local/bin/picoquicdemo && \
    cp build/qgf-r17-peer /usr/local/bin/qgf-r17-peer && \
    dpkg-query -W > /src/build-packages.txt

WORKDIR /repo
ARG QGF_SOURCE_COMMIT
ARG QGF_SOURCE_TREE
ENV QGF_SOURCE_COMMIT=$QGF_SOURCE_COMMIT QGF_SOURCE_TREE=$QGF_SOURCE_TREE
ENV GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go test -c -tags=picoquic -o /src/peer-control.test ./integrationtests/self
RUN go test -c -tags=picoquic -o /src/peer-smoke.test .
RUN cp /src/peer-smoke.test /src/peer-matrix.test
RUN PYTHONDONTWRITEBYTECODE=1 python3 -B -m unittest discover -s interop/multipath -p test_run.py && \
    go test -tags=picoquic -run='^TestPicoquicMatrix(AwaitsTraceConsumption|CIDReceipt|ReplayDispositions|MTUFinalLossObservation|MTUProgress)$' -count=1 .
ENTRYPOINT ["python3", "interop/multipath/run.py", "--binary", "/usr/local/bin/picoquicdemo"]
