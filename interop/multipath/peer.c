/* R17 application/UDP adapter for the pinned picoquic library. QUIC parsing,
 * protection, recovery and path state belong exclusively to picoquic. The
 * two-byte command header is private fixture application data, not QUIC.
 */
#include "picoquic.h"
#include "picoquic_utils.h"

#include <arpa/inet.h>
#include <errno.h>
#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>

#define COMMAND_LIMIT 65536
#define STREAM_LIMIT 64
#define ALPN "qgf-r17"
#define TUPLE_GATE_LIMIT 64

typedef struct {
    uint64_t id;
    size_t length;
    unsigned char bytes[COMMAND_LIMIT];
    int used;
} command_t;

typedef struct {
    unsigned char bytes[PICOQUIC_MAX_PACKET_SIZE];
    size_t length;
    struct sockaddr_storage from, local;
} gated_datagram_t;

typedef struct {
    picoquic_quic_t *quic;
    picoquic_cnx_t *cnx;
    int sockets[3];
    struct sockaddr_in addresses[3];
    uint64_t paths[8];
    size_t path_count;
    command_t commands[STREAM_LIMIT];
    int closed, failed, rebind, hold, limit_mtu, mtu_drops, blackhole_zero;
    int open_pending;
    uint64_t open_stream;
    unsigned char held[PICOQUIC_MAX_PACKET_SIZE];
    size_t held_length;
    struct sockaddr_storage held_to;
    int held_socket;
    unsigned char datagram[1500];
    size_t datagram_length;
    uint64_t datagram_path;
    int is_client, gate_armed, gate_released, gate_release_pending;
    struct sockaddr_in gate_from;
    size_t gate_count;
    uint64_t gate_stream;
    gated_datagram_t gate[TUPLE_GATE_LIMIT];
    /* Temporary terminal-only R17 E0 boundary evidence; no transport state. */
    int diagnostic, diagnostic_emitted, diagnostic_accepted;
    int diagnostic_affinity_called, diagnostic_affinity_status;
    int diagnostic_queue_called, diagnostic_queue_status;
    uint64_t diagnostic_stream;
    size_t diagnostic_request, diagnostic_response;
} driver_t;

static void diagnostic_terminal(driver_t *d, const char *boundary)
{
    if (!d->diagnostic || d->diagnostic_emitted) return;
    d->diagnostic_emitted = 1;
    fprintf(stdout, "[R17E0] NOT QUALIFICATION terminal %s accepted-fin %d stream %llu request %zu response %zu affinity-called %d affinity-status %d queue-called %d queue-status %d\n",
        boundary, d->diagnostic_accepted, (unsigned long long)d->diagnostic_stream,
        d->diagnostic_request, d->diagnostic_response,
        d->diagnostic_affinity_called, d->diagnostic_affinity_status,
        d->diagnostic_queue_called, d->diagnostic_queue_status);
}

static int gate_failed(driver_t *d, const char *reason)
{
    fprintf(stderr, "tuple gate failed: %s retained %zu\n", reason, d->gate_count);
    d->failed = 1;
    return -1;
}

static int same_address(const struct sockaddr_storage *a, const struct sockaddr_in *b)
{
    const struct sockaddr_in *v4 = (const struct sockaddr_in *)a;
    return a->ss_family == AF_INET && v4->sin_addr.s_addr == b->sin_addr.s_addr && v4->sin_port == b->sin_port;
}

static uint64_t packet_path(driver_t *d, struct sockaddr_storage *from, struct sockaddr_storage *to)
{
    for (size_t i = 0; i < d->path_count; i++) {
        struct sockaddr_storage local = {0}, remote = {0};
        if (picoquic_get_path_addr(d->cnx, d->paths[i], 1, &local) == 0 &&
            picoquic_get_path_addr(d->cnx, d->paths[i], 2, &remote) == 0 &&
            same_address(&local, (struct sockaddr_in *)from) && same_address(&remote, (struct sockaddr_in *)to)) return d->paths[i];
    }
    return 0;
}

static int command(driver_t *d, uint64_t stream_id, const unsigned char *bytes, size_t length)
{
    if (length < 2) return -1;
    const unsigned char op = bytes[0], path = bytes[1];
    int ret = 0;
    struct sockaddr *remote = NULL;
    uint64_t now = picoquic_get_quic_time(d->quic);
    fprintf(stdout, "command %c path %u\n", op, path);
    switch (op) {
    case 'G':
        if (d->is_client || path != 2 || length != 8 || d->gate_armed || d->gate_released) return gate_failed(d, "invalid arm");
        d->gate_from.sin_family = AF_INET;
        memcpy(&d->gate_from.sin_addr.s_addr, bytes + 2, 4);
        memcpy(&d->gate_from.sin_port, bytes + 6, 2);
        if (d->gate_from.sin_addr.s_addr != htonl(0x7f000003u) || d->gate_from.sin_port == 0) return gate_failed(d, "invalid replacement tuple");
        d->gate_armed = 1;
        fprintf(stdout, "tuple gate armed replacement port %u capacity %d\n", ntohs(d->gate_from.sin_port), TUPLE_GATE_LIMIT);
        break;
    case 'g':
        if (d->is_client || path != 2 || length != 2 || !d->gate_armed || d->gate_release_pending) return gate_failed(d, "invalid release");
        /* Do not reenter picoquic from its stream callback. The loop owns
         * FIFO delivery and acknowledges this RPC only after it drains. */
        d->gate_release_pending = 1;
        d->gate_stream = stream_id;
        return 0;
    case 'E': {
        int observe = d->diagnostic && d->is_client && d->limit_mtu && path == 0 && !d->diagnostic_accepted;
        if (observe) {
            /* command is entered only after the original request FIN callback. */
            d->diagnostic_accepted = 1;
            d->diagnostic_stream = stream_id;
            d->diagnostic_request = length;
            d->diagnostic_response = length - 2;
        }
        if (path != 255) {
            ret = picoquic_set_stream_path_affinity(d->cnx, stream_id, path);
            if (observe) { d->diagnostic_affinity_called = 1; d->diagnostic_affinity_status = ret; }
        }
        if (ret == 0) {
            ret = picoquic_add_to_stream(d->cnx, stream_id, bytes + 2, length - 2, 1);
            if (observe) { d->diagnostic_queue_called = 1; d->diagnostic_queue_status = ret; }
            return ret;
        }
        break;
    }
    case 'O':
        if (d->open_pending) return -1;
        int allowed = 0;
        ret = picoquic_subscribe_new_path_allowed(d->cnx, &allowed);
        if (ret != 0) break;
        if (!allowed) { d->open_pending = 1; d->open_stream = stream_id; return 0; }
        picoquic_get_peer_addr(d->cnx, &remote);
        ret = picoquic_probe_new_path(d->cnx, remote, (struct sockaddr *)&d->addresses[1], now);
        break;
    case 'B':
        ret = picoquic_set_path_status(d->cnx, path, picoquic_path_status_backup);
        break;
    case 'V':
        ret = picoquic_set_path_status(d->cnx, path, picoquic_path_status_available);
        break;
    case 'A':
        ret = picoquic_abandon_path(d->cnx, path, 0, now);
        break;
    case 'K':
        ret = picoquic_start_key_rotation(d->cnx);
        break;
    case 'Z': {
        /* Draft-21 duplicate ACKs for packet zero, received before this
         * command. ACK and PATH_ACK both target path zero. */
        const unsigned char ack[] = {0x02, 0, 0, 0, 0};
        const unsigned char path_ack[] = {0x3e, 0, 0, 0, 0, 0};
        ret = picoquic_queue_misc_frame(d->cnx, ack, sizeof(ack), 1, picoquic_packet_context_application);
        if (ret == 0) ret = picoquic_queue_misc_frame(d->cnx, path_ack, sizeof(path_ack), 1, picoquic_packet_context_application);
        break;
    }
    case 'H':
        d->hold = 1;
        break;
    case 'Q': {
        /* Typed fixture metadata: the size of the one opaque held datagram. */
        const unsigned char size[] = {(unsigned char)(d->held_length >> 8), (unsigned char)d->held_length};
        return picoquic_add_to_stream(d->cnx, stream_id, size, sizeof(size), 1);
    }
    case 'R':
        if (d->held_length == 0) return -1;
        if (sendto(d->sockets[d->held_socket], d->held, d->held_length, 0,
                   (struct sockaddr *)&d->held_to, sizeof(struct sockaddr_in)) != (ssize_t)d->held_length) return -1;
        fprintf(stdout, "replayed path-one packet %zu bytes\n", d->held_length);
        d->held_length = 0;
        /* Explicit late feedback targets retired path one through a live
         * protected carrier. Packet zero was sent before path retirement. */
        const unsigned char late_ack[] = {0x3e, 1, 0, 0, 0, 0};
        ret = picoquic_queue_misc_frame(d->cnx, late_ack, sizeof(late_ack), 1, picoquic_packet_context_application);
        break;
    case 'N':
        /* NAT changes the socket mapping; picoquic retains its original
         * tuple and path identity. Incoming addresses map back below. */
        d->rebind = 1;
        break;
    case 'M':
        d->limit_mtu = 1;
        break;
    case 'F':
        /* Reply on the healthy path without changing the Go endpoint's
         * local policy. Its next primary-path request must trigger failover. */
        ret = picoquic_set_stream_path_affinity(d->cnx, stream_id, 1);
        if (ret == 0) d->blackhole_zero = 1;
        break;
    case 'I': {
        /* Invalid draft-21 PATH_STATUS_BACKUP for a never-granted path ID. */
        const unsigned char invalid[] = {0x7e, 0x76, 0x40, 0x7f, 0};
        ret = picoquic_queue_misc_frame(d->cnx, invalid, sizeof(invalid), 0, picoquic_packet_context_application);
        break;
    }
    default:
        return -1;
    }
    if (ret != 0) { fprintf(stderr, "command %c failed: %d\n", op, ret); return ret; }
    const unsigned char ok[] = {'O', 'K'};
    return picoquic_add_to_stream(d->cnx, stream_id, ok, sizeof(ok), 1);
}

static int callback(picoquic_cnx_t *cnx, uint64_t stream_id, uint8_t *bytes, size_t length,
                    picoquic_call_back_event_t event, void *context, void *stream_context)
{
    (void)stream_context;
    driver_t *d = context;
    d->cnx = cnx;
    switch (event) {
    case picoquic_callback_stream_data:
    case picoquic_callback_stream_fin: {
        command_t *c = NULL;
        for (size_t i = 0; i < STREAM_LIMIT; i++) {
            if (d->commands[i].used && d->commands[i].id == stream_id) { c = &d->commands[i]; break; }
            if (!d->commands[i].used && c == NULL) c = &d->commands[i];
        }
        if (c == NULL || c->length + length > COMMAND_LIMIT) return -1;
        c->used = 1;
        c->id = stream_id;
        if (length != 0) memcpy(c->bytes + c->length, bytes, length);
        c->length += length;
        if (event == picoquic_callback_stream_fin) {
            int ret = command(d, stream_id, c->bytes, c->length);
            c->used = 0;
            c->length = 0;
            if (ret != 0) { d->failed = 1; return ret; }
        }
        break;
    }
    case picoquic_callback_datagram:
        fprintf(stdout, "datagram path %llu length %zu\n", (unsigned long long)stream_id, length);
        if (length == 0 || length > sizeof(d->datagram) || d->datagram_length != 0) return -1;
        memcpy(d->datagram, bytes, length);
        d->datagram_length = length;
        d->datagram_path = bytes[0];
        return picoquic_mark_datagram_ready_path(cnx, d->datagram_path, 1);
    case picoquic_callback_prepare_datagram: {
        if (stream_id != d->datagram_path || d->datagram_length == 0 || length < d->datagram_length) return -1;
        unsigned char *buffer = picoquic_provide_datagram_buffer_ex(bytes, d->datagram_length, picoquic_datagram_not_active);
        if (buffer == NULL) return -1;
        memcpy(buffer, d->datagram, d->datagram_length);
        d->datagram_length = 0;
        break;
    }
    case picoquic_callback_ready:
        fprintf(stdout, "connection ready\n");
        break;
    case picoquic_callback_path_available:
        for (size_t i = 0; i < d->path_count; i++) if (d->paths[i] == stream_id) return 0;
        if (d->path_count >= 8) return -1;
        d->paths[d->path_count++] = stream_id;
        fprintf(stdout, "path available %llu\n", (unsigned long long)stream_id);
        break;
    case picoquic_callback_path_deleted:
        fprintf(stdout, "path deleted %llu\n", (unsigned long long)stream_id);
        break;
    case picoquic_callback_next_path_allowed:
        if (d->open_pending) {
            struct sockaddr *remote = NULL;
            picoquic_get_peer_addr(cnx, &remote);
            int ret = picoquic_probe_new_path(cnx, remote, (struct sockaddr *)&d->addresses[1], picoquic_get_quic_time(d->quic));
            if (ret != 0) { d->failed = 1; return ret; }
            d->open_pending = 0;
            return picoquic_add_to_stream(cnx, d->open_stream, (const uint8_t *)"OK", 2, 1);
        }
        break;
    case picoquic_callback_close:
    case picoquic_callback_application_close:
    case picoquic_callback_stateless_reset:
        fprintf(stdout, "closed local %llu remote %llu\n", (unsigned long long)picoquic_get_local_error(cnx),
                (unsigned long long)picoquic_get_remote_error(cnx));
        d->closed = 1;
        diagnostic_terminal(d, "connection-close");
        break;
    default:
        break;
    }
    return 0;
}

static int run(driver_t *d)
{
    uint64_t deadline = picoquic_get_quic_time(d->quic) + 30000000;
    while (!d->closed && !d->failed && picoquic_get_quic_time(d->quic) < deadline) {
        if (d->gate_release_pending) {
            fprintf(stdout, "tuple gate releasing %zu datagrams\n", d->gate_count);
            for (size_t i = 0; i < d->gate_count; i++) {
                gated_datagram_t *packet = &d->gate[i];
                if (picoquic_incoming_packet(d->quic, packet->bytes, packet->length,
                        (struct sockaddr *)&packet->from, (struct sockaddr *)&packet->local,
                        0, 0, picoquic_get_quic_time(d->quic)) != 0) return gate_failed(d, "incoming packet API");
                if (d->failed || d->closed) return gate_failed(d, "connection ended during drain");
            }
            d->gate_armed = 0;
            d->gate_released = 1;
            d->gate_release_pending = 0;
            const unsigned char ok[] = {'O', 'K'};
            if (picoquic_add_to_stream(d->cnx, d->gate_stream, ok, sizeof(ok), 1) != 0) return gate_failed(d, "release response API");
            fprintf(stdout, "tuple gate drained %zu datagrams; API statuses zero\n", d->gate_count);
            d->gate_count = 0;
        }
        uint8_t buffer[PICOQUIC_MAX_PACKET_SIZE];
        for (int batch = 0; batch < 64; batch++) {
            struct sockaddr_storage to = {0}, from = {0};
            size_t length = 0;
            int if_index = 0;
            picoquic_cnx_t *last = NULL;
            picoquic_connection_id_t log_id;
            if (picoquic_prepare_next_packet(d->quic, picoquic_get_quic_time(d->quic), buffer, sizeof(buffer), &length,
                                            &to, &from, &if_index, &log_id, &last) != 0) return -1;
            if (length == 0) break;
            int socket_index = 0;
            for (int i = 0; i < 3; i++) if (same_address(&from, &d->addresses[i])) socket_index = i;
            uint64_t path = d->cnx ? packet_path(d, &from, &to) : 0;
            if (d->rebind && socket_index == 1) socket_index = 2;
            if (d->blackhole_zero && path == 0) continue;
            if (d->hold && path == 1) {
                memcpy(d->held, buffer, length);
                d->held_length = length;
                d->held_to = to;
                d->held_socket = socket_index;
                d->hold = 0;
                fprintf(stdout, "held path-one packet %zu bytes\n", length);
                continue;
            }
            if (d->limit_mtu && path == 1 && length > 1280) { d->mtu_drops++; continue; }
            if (sendto(d->sockets[socket_index], buffer, length, 0,
                       (struct sockaddr *)&to, sizeof(struct sockaddr_in)) != (ssize_t)length) return -1;
        }
        struct pollfd fds[3];
        for (int i = 0; i < 3; i++) { fds[i].fd = d->sockets[i]; fds[i].events = POLLIN; fds[i].revents = 0; }
        int64_t delay = picoquic_get_next_wake_delay(d->quic, picoquic_get_quic_time(d->quic), 10000);
        int ready = poll(fds, 3, (int)((delay + 999) / 1000));
        if (ready < 0 && errno != EINTR) return -1;
        for (int i = 0; i < 3; i++) {
            if (!(fds[i].revents & POLLIN)) continue;
            struct sockaddr_storage from = {0};
            socklen_t from_length = sizeof(from);
            ssize_t length = recvfrom(d->sockets[i], buffer, sizeof(buffer), d->gate_armed ? MSG_TRUNC : 0, (struct sockaddr *)&from, &from_length);
            if (length < 0) return -1;
            if (d->gate_armed && (size_t)length > sizeof(buffer)) return gate_failed(d, "oversized or truncated datagram");
            int virtual_socket = d->rebind && i == 2 ? 1 : i;
            struct sockaddr_storage local = {0};
            memcpy(&local, &d->addresses[virtual_socket], sizeof(struct sockaddr_in));
            if (d->gate_armed && i == 0 && same_address(&from, &d->gate_from)) {
                if (length == 0 || d->gate_count == TUPLE_GATE_LIMIT) return gate_failed(d, "empty datagram or capacity overflow");
                gated_datagram_t *packet = &d->gate[d->gate_count++];
                memcpy(packet->bytes, buffer, (size_t)length);
                packet->length = (size_t)length;
                packet->from = from;
                packet->local = local;
                continue;
            }
            uint64_t path = d->cnx ? packet_path(d, &local, &from) : 0;
            if (d->blackhole_zero && path == 0) continue;
            if (d->limit_mtu && path == 1 && length > 1280) { d->mtu_drops++; continue; }
            if (picoquic_incoming_packet(d->quic, buffer, (size_t)length, (struct sockaddr *)&from,
                                        (struct sockaddr *)&d->addresses[virtual_socket], 0, 0,
                                        picoquic_get_quic_time(d->quic)) != 0) return -1;
        }
    }
    fprintf(stdout, "driver closed %d failed %d mtu-drops %d\n", d->closed, d->failed, d->mtu_drops);
    return d->closed && !d->failed ? 0 : -1;
}

int main(int argc, char **argv)
{
    /* role, local port, remote port, certificate, key, CA, multipath(0/1) */
    if (argc != 8) return 2;
    setbuf(stdout, NULL);
    driver_t *d = calloc(1, sizeof(*d));
    if (d == NULL) return 2;
    int client = strcmp(argv[1], "client") == 0;
    d->is_client = client;
    const char *diagnostic = getenv("QGF_R17_DIAGNOSTIC");
    d->diagnostic = diagnostic != NULL && strcmp(diagnostic, "terminal-e0-v1") == 0;
    for (int i = 0; i < 3; i++) {
        d->sockets[i] = socket(AF_INET, SOCK_DGRAM, 0);
        d->addresses[i].sin_family = AF_INET;
        d->addresses[i].sin_port = htons(i == 0 ? (uint16_t)atoi(argv[2]) : 0);
        d->addresses[i].sin_addr.s_addr = htonl(0x7f000001u + (unsigned)i);
        if (d->sockets[i] < 0 || bind(d->sockets[i], (struct sockaddr *)&d->addresses[i], sizeof(struct sockaddr_in)) != 0) return 2;
        socklen_t length = sizeof(struct sockaddr_in);
        if (getsockname(d->sockets[i], (struct sockaddr *)&d->addresses[i], &length) != 0) return 2;
    }
    d->quic = picoquic_create(1, client ? NULL : argv[4], client ? NULL : argv[5], argv[6], ALPN,
                              callback, d, NULL, NULL, NULL, picoquic_current_time(), NULL, NULL, NULL, 0);
    if (d->quic == NULL) return 2;
    picoquic_set_client_cert_verification_policy(d->quic, 1);
    picoquic_enable_path_callbacks_default(d->quic, 1);
    picoquic_tp_t tp = *picoquic_get_default_tp(d->quic);
    tp.initial_max_path_id = atoi(argv[7]) ? 1 : 0;
    tp.max_datagram_frame_size = 1500;
    tp.max_idle_timeout = 10000;
    if (picoquic_set_default_tp(d->quic, &tp) != 0) return 2;
    d->paths[d->path_count++] = 0;
    if (client) {
        struct sockaddr_in remote = {.sin_family = AF_INET, .sin_port = htons((uint16_t)atoi(argv[3]))};
        remote.sin_addr.s_addr = htonl(0x7f000001u);
        d->cnx = picoquic_create_cnx(d->quic, picoquic_null_connection_id, picoquic_null_connection_id,
                                    (struct sockaddr *)&remote, picoquic_current_time(), 1, "localhost", ALPN, 1);
        if (d->cnx == NULL || picoquic_set_local_addr(d->cnx, (struct sockaddr *)&d->addresses[0]) != 0 ||
            picoquic_start_client_cnx(d->cnx) != 0) return 2;
    }
    fprintf(stdout, "Waiting for packets.\n");
    int ret = run(d);
    diagnostic_terminal(d, "run-return");
    picoquic_free(d->quic);
    for (int i = 0; i < 3; i++) close(d->sockets[i]);
    free(d);
    return ret == 0 ? 0 : 1;
}
