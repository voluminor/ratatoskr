#!/usr/bin/env python3
import ipaddress
import socket
import struct
import sys


def fail(message):
    print(message, file=sys.stderr)
    return 1


def split_host_port(value):
    if value.startswith("["):
        end = value.index("]")
        return value[1:end], int(value[end + 2 :])
    host, port = value.rsplit(":", 1)
    return host, int(port)


def read_exact(conn, size):
    data = bytearray()
    while len(data) < size:
        chunk = conn.recv(size - len(data))
        if not chunk:
            raise RuntimeError("short SOCKS reply")
        data.extend(chunk)
    return bytes(data)


def parse_socks_addr(conn, atyp):
    if atyp == 0x01:
        host = socket.inet_ntop(socket.AF_INET, read_exact(conn, 4))
    elif atyp == 0x04:
        host = socket.inet_ntop(socket.AF_INET6, read_exact(conn, 16))
    elif atyp == 0x03:
        length = read_exact(conn, 1)[0]
        host = read_exact(conn, length).decode("ascii")
    else:
        raise RuntimeError(f"unsupported SOCKS address type {atyp}")
    port = struct.unpack("!H", read_exact(conn, 2))[0]
    return host, port


def encode_udp_header(address):
    host, port = split_host_port(address)
    ip = ipaddress.ip_address(host)
    if ip.version == 4:
        return b"\x00\x00\x00\x01" + ip.packed + struct.pack("!H", port)
    return b"\x00\x00\x00\x04" + ip.packed + struct.pack("!H", port)


def parse_udp_payload(packet):
    if len(packet) < 10 or packet[:2] != b"\x00\x00" or packet[2] != 0:
        raise RuntimeError("invalid SOCKS UDP header")
    atyp = packet[3]
    offset = 4
    if atyp == 0x01:
        offset += 4
    elif atyp == 0x04:
        offset += 16
    elif atyp == 0x03:
        offset += 1 + packet[offset]
    else:
        raise RuntimeError(f"unsupported SOCKS UDP address type {atyp}")
    offset += 2
    if len(packet) < offset:
        raise RuntimeError("truncated SOCKS UDP packet")
    return packet[offset:]


def main(argv):
    if len(argv) != 5:
        return fail("usage: socks-udp-check.py SOCKS_HOST SOCKS_PORT DEST_ADDR PAYLOAD")

    socks_host, socks_port, dest_addr, payload = argv[1], int(argv[2]), argv[3], argv[4].encode()
    with socket.create_connection((socks_host, socks_port), timeout=5) as control:
        control.settimeout(5)
        control.sendall(b"\x05\x01\x00")
        if read_exact(control, 2) != b"\x05\x00":
            return fail("SOCKS no-auth negotiation failed")

        control.sendall(b"\x05\x03\x00\x01\x00\x00\x00\x00\x00\x00")
        head = read_exact(control, 4)
        if head[0] != 0x05 or head[1] != 0x00:
            return fail(f"SOCKS UDP associate failed: {head.hex()}")
        relay_host, relay_port = parse_socks_addr(control, head[3])
        if relay_host in ("0.0.0.0", "::"):
            relay_host = socks_host

        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as udp:
            udp.settimeout(5)
            packet = encode_udp_header(dest_addr) + payload
            udp.sendto(packet, (relay_host, relay_port))
            data, _ = udp.recvfrom(65535)
            got = parse_udp_payload(data)
            if got != payload:
                return fail(f"unexpected UDP payload: {got!r}")
            print(got.decode("utf-8", "replace"))
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
