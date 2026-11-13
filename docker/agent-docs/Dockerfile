# Runtime only. build.sh produces the Next standalone output before this image is built.
ARG NODE_RUNTIME=172.30.10.185:15000/common/node:22-chrome
FROM ${NODE_RUNTIME}

ENV DEPLOY_DIR=/dianyi/app/agent-docs
WORKDIR $DEPLOY_DIR

COPY apps/docs/.next/standalone/ ./
COPY apps/docs/.next/static ./apps/docs/.next/static
COPY apps/docs/public ./apps/docs/public
COPY docker/agent-docs/start.sh ./start.sh
COPY docker/runtime-limits.sh ./runtime-limits.sh
COPY docker/company-log.sh ./company-log.sh
RUN chmod 755 ./start.sh ./runtime-limits.sh ./company-log.sh

EXPOSE 80
CMD ["./start.sh"]
