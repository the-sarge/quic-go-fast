# The driver structure follows qoke/mp-quic-go at 084c13ea3834fbe58cedb63db9208951d714f839 (MIT).
# Ordinary control, R14 smoke and R17's fixed independent matrix share these pins.
FROM golang:1.27.3-bookworm@sha256:d9b6a1beb9474adc2a1ce842573639b44b2a0dd49b6669f51d3821baf9ec37c9

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
    git fetch -q --depth 1 origin 01e124ebdf9fc50df307e01e71f2744471ddc470 && git checkout -q FETCH_HEAD && \
    test "$(git rev-parse HEAD)" = 01e124ebdf9fc50df307e01e71f2744471ddc470 && \
    printf '%s\n' 'add_executable(qgf-r17-peer /src/r17-peer.c)' \
      'target_include_directories(qgf-r17-peer PRIVATE ${PTLS_INCLUDE_DIRS})' \
      'target_link_libraries(qgf-r17-peer PRIVATE picoquic-core)' >> CMakeLists.txt && \
    cmake -S . -B build -DCMAKE_BUILD_TYPE=Release -DPICOQUIC_FETCH_PTLS=ON \
      -DPICOQUIC_FETCH_PTLS_TAG=bfa67875982afc4c24f21e146cef4747fa189c2f -DBUILD_TESTING=OFF && \
    test "$(git -C build/_deps/picotls-src rev-parse HEAD)" = bfa67875982afc4c24f21e146cef4747fa189c2f && \
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
ENTRYPOINT ["python3", "interop/multipath/run.py", "--binary", "/usr/local/bin/picoquicdemo"]
