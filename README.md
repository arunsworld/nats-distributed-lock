# Distributed Locking using NATS

A distributed lock allows any number of independendent processes to synchronize when doing some work, such that only one process is elected to do the work at any given point in time.

The guarantee offered by this library is that at no point will 2 processes be elected leader for the same piece of work. The trade-off is that for a brief period of time (configurable), no node might be leader.

The heavy lifting is done by NATS with the library utilizing it's KV functionality.

There are many ways to setup NATS; a few are captured in the nats-setups directory provided as a reference and not part of the library code.