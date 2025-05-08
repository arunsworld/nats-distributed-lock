# Docker

```bash
docker volume create nats

docker run --name nats --restart unless-stopped -d -p 4222:4222 -v $PWD:/conf -v nats:/data nats -c /conf/server.conf
```