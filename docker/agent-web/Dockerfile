# Runtime only. build.sh produces the Next standalone output before this image is built.
# This is the Node 22 runtime, not common/nginx.
ARG NODE_RUNTIME=172.30.10.185:15000/common/node:22
FROM ${NODE_RUNTIME}

ENV DEPLOY_DIR=/dianyi/app/agent-web
WORKDIR $DEPLOY_DIR

COPY apps/web/.next/standalone/ ./
COPY apps/web/.next/static ./apps/web/.next/static
COPY apps/web/public ./apps/web/public
COPY docker/agent-web/start.sh ./start.sh
COPY docker/runtime-limits.sh ./runtime-limits.sh
COPY docker/company-log.sh ./company-log.sh
RUN chmod 755 ./start.sh ./runtime-limits.sh ./company-log.sh

EXPOSE 80
CMD ["./start.sh"]
