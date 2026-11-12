# Runtime only. build.sh compiles the binaries before this image is built.
# Node and nginx images are not used here. Override GO_RUNTIME if the registry tag differs.
ARG GO_RUNTIME=172.30.10.185:15000/common/golang:1.27.0
FROM ${GO_RUNTIME}

ENV APP_HOME=/dianyi/app/agent-api
WORKDIR $APP_HOME

COPY docker/agent-api/out/server docker/agent-api/out/migrate ./
COPY server/migrations ./migrations/
COPY server/config ./config/
COPY docker/agent-api/start.sh ./start.sh
COPY docker/runtime-limits.sh ./runtime-limits.sh
COPY docker/company-log.sh ./company-log.sh
RUN chmod 755 ./start.sh ./runtime-limits.sh ./company-log.sh ./server ./migrate

EXPOSE 8080
CMD ["./start.sh"]
