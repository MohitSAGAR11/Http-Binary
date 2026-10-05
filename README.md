# bhttp — HTTP, in binary

A small binary protocol for HTTP-style requests over TCP, with two programs:

- **`bserve`**: a file server. It answers requests for files under a root directory and keeps the connection open.
- **`bcurl`**: a client. It sends requests over one TCP connection, writes response bodies to stdout and can hexdump every frame.

## Build

Requires Go 1.24+.

```sh
go build -o bserve ./cmd/bserve
go build -o bcurl  ./cmd/bcurl
```

On Windows, use `-o bserve.exe` and `-o bcurl.exe`.

## Run

```sh
./bserve ./www 9000
```

```sh
./bcurl -v localhost:9000/index.html
./bcurl localhost:9000/ localhost:9000/hello.txt localhost:9000/missing
```

- The response body goes to stdout. `-v` hexdumps every frame sent (`>`) and received (`<`) to stderr.
- You can give several URLs, but they must use the same `host:port`. They all go over **one** connection as streams 1, 2, 3, and so on.
- If no port is given, it defaults to `9000`. A leading `bin://` or `http://` is accepted and ignored.
- Exit codes: `0` if every response is under 400, `1` if any response is 4xx/5xx, `2` on a usage, network or protocol error.

## Layout

```
wire/frame.go       frame header: read / write
wire/headers.go     header block: encode / decode, static table
cmd/bserve/main.go  server
cmd/bcurl/main.go   client
www/                sample root
```

---

# The spec

## 1. Transport

- Plain TCP. All integers are **big-endian**, and every field is byte-aligned.
- The client opens one connection and sends requests on it one after another.
- The server keeps the connection open after every response, including error responses.
- The server closes the connection only when the client closes it or when a frame is cut off mid-header or mid-payload. After a cut-off frame there is no way to find where the next frame starts.

## 2. Frame header: 8 bytes, fixed

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+---------------------------------------------------------------+
|                        Length (32)                            |
+---------------+---------------+-------------------------------+
|   Type (8)    |   Flags (8)   |         Stream ID (16)        |
+---------------+---------------+-------------------------------+
|                    Payload (Length bytes) ...                 |
+---------------------------------------------------------------+
```

| Field     | Bits | Meaning |
|-----------|------|---------|
| Length    | 32   | Payload length in bytes. The 8-byte header is not counted. |
| Type      | 8    | `0x01` HEADERS, `0x02` DATA. Every other value is reserved. |
| Flags     | 8    | `0x01` END_STREAM: the last frame of this request or response. Other bits are reserved: senders send 0, receivers ignore them. |
| Stream ID | 16   | Chosen by the client. It starts at 1 and goes up by one per request. The server echoes it in the response. |

A receiver must accept payloads up to **16 MiB** (2^24 bytes). If a frame is larger, the receiver reads and discards the payload, then treats the frame as malformed.

### Why these widths

HTTP/2 uses **24 / 8 / 8 / 31** (plus 1 reserved bit), which makes a 9-byte header. It needs:

- **24-bit length**, because HTTP/2 multiplexes many streams on one connection. Frames are kept small (16 KiB by default) so one large response can't block the others. 24 bits is enough for that, and the extra byte isn't worth sending.
- **31-bit stream ID**, because one HTTP/2 connection can stay open for hours and carry millions of streams. Stream IDs are never reused. The reserved top bit keeps the value positive in languages with only signed 32-bit integers.

We picked **32 / 8 / 8 / 16**, which makes an 8-byte header:

- **32-bit length**: it keeps the header 8 bytes and byte-aligned, so a hexdump reads in clean columns. The real limit is the 16 MiB receiver cap, not the field width. The cost is 1 byte per frame.
- **8-bit type**: 256 frame types leave plenty of room for a version 2 (see section 5).
- **8-bit flags**: we use one flag and keep seven for later.
- **16-bit stream ID**: we don't multiplex, and requests run one after another, so 65,535 requests per connection is plenty. If a client needs more, it opens a new connection.

## 3. Header block: the HEADERS payload

The HEADERS payload is a list of entries, one after another, until the payload ends.

```
indexed name:  [ idx (1..10) ]               [ vlen (16) ][ value ]
literal name:  [ 0x00 ][ nlen (8) ][ name ]  [ vlen (16) ][ value ]
```

Static table: the ten names we actually send.

| idx | name             | idx | name           |
|-----|------------------|-----|----------------|
| 1   | `:method`        | 6   | `host`         |
| 2   | `:path`          | 7   | `user-agent`   |
| 3   | `:status`        | 8   | `server`       |
| 4   | `content-type`   | 9   | `connection`   |
| 5   | `content-length` | 10  | `date`         |

- Names are lowercase ASCII. A literal name is 1 to 255 bytes long. A value is 0 to 65,535 bytes long.
- Index bytes `11`–`255` are reserved. A block that uses one is malformed.
- A block is malformed if an entry runs past the end of the payload.

These are the first two mechanisms of HPACK: a static table and length-prefixed literals. We left out the dynamic table and Huffman coding.

## 4. Messages

**Request.** One HEADERS frame with END_STREAM set.

- It must include `:method` and `:path`. `:path` must start with `/`.
- Requests have no body. The server ignores any DATA frame a client sends.

**Response.** One HEADERS frame, then zero or more DATA frames, all on the request's stream ID.

- `:status` is an ASCII decimal number, for example `"200"`.
- The last frame of the response has END_STREAM set. If the body is empty, that frame is the HEADERS frame.
- `bserve` sends DATA in chunks of up to 16 KiB.

**Status codes the server sends:**

| Status | When |
|--------|------|
| 200 | The file exists. If the path is a directory, the server serves `index.html` from it. |
| 400 | The frame is malformed: a bad header block, a missing `:method` or `:path`, or a payload over 16 MiB. |
| 404 | The file doesn't exist, or the path tries to escape the root (`../`). |
| 405 | The method isn't `GET`. |

## 5. Unknown frames: the version-2 rule

> **A receiver that meets a frame type it does not know MUST read `Length` bytes, discard them, and carry on.**

It must not raise an error or close the connection. This rule lets a future version add new frame types without breaking old peers. It works only because every frame starts with its length, even frames the receiver can't understand.

---

# Annotated hexdump

This is real output from `bcurl -v localhost:9000/index.html`.

### Request: client → server (55 bytes)

```
00 00 00 2f        Length     = 47
01                 Type       = HEADERS
01                 Flags      = END_STREAM
00 01              Stream ID  = 1
-- header block (47 bytes) --
01                 idx 1  :method
00 03 47 45 54       len 3   "GET"
02                 idx 2  :path
00 0b 2f 69 6e 64 65 78 2e 68 74 6d 6c
                     len 11  "/index.html"
06                 idx 6  host
00 0e 6c 6f 63 61 6c 68 6f 73 74 3a 39 30 30 30
                     len 14  "localhost:9000"
07                 idx 7  user-agent
00 07 62 63 75 72 6c 2f 31
                     len 7   "bcurl/1"
```

### Response frame 1: HEADERS (58 bytes)

```
00 00 00 32        Length     = 50
01                 Type       = HEADERS
00                 Flags      = none (DATA follows)
00 01              Stream ID  = 1
-- header block (50 bytes) --
03                 idx 3  :status
00 03 32 30 30       len 3   "200"
08                 idx 8  server
00 08 62 73 65 72 76 65 2f 31
                     len 8   "bserve/1"
04                 idx 4  content-type
00 18 74 65 78 74 2f 68 74 6d 6c 3b 20 63 68 61 72 73 65 74 3d 75 74 66 2d 38
                     len 24  "text/html; charset=utf-8"
05                 idx 5  content-length
00 03 31 30 36       len 3   "106"
```

### Response frame 2: DATA (114 bytes)

```
00 00 00 6a        Length     = 106
02                 Type       = DATA
01                 Flags      = END_STREAM (response complete)
00 01              Stream ID  = 1
3c 21 64 6f 63 74 79 70 65 20 68 74 6d 6c 3e 0a ...
                   106 bytes of the file: "<!doctype html>\n<html>..."
```

Total on the wire: 55 bytes up and 172 bytes down. The response is the 106-byte body plus a 50-byte header block plus two 8-byte frame headers.
