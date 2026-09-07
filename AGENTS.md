# Working on ERL

Read `CONTRIBUTING.md` for the development workflow and `README.md` for the public
routing contract.

- Keep handlers and middleware compatible with `net/http`. The runtime library
  must remain dependency-free and use public Go APIs.
- Register routes before serving. Preserve middleware snapshots, path precedence,
  segment-wise decoding, and the original response writer.
- Do not pool request state that a standard Go handler could retain after returning.
- Run `make check` for code changes, and `make fuzz` for matcher or parser changes.
  Build `make examples` when changing the public API or example code.
- Keep the examples runnable and update documentation when behavior changes.
- Read `bench/README.md` before performance work. Keep the comparison workload
  fixed, retain raw measurements, and distinguish route-dispatch improvements from
  statistically supported HTTP/2 throughput improvements.
