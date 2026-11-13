#!/bin/bash
# Build on this machine, copy the images onto the cluster node, then start them.
# The node does not pull from Harbor or Docker Hub.
set -euo pipefail

root="$(cd "$(dirname "$0")" && pwd)"
export KUBECONFIG="${KUBECONFIG:-/Users/zhujinqi/Documents/env/pem/config}"

namespace="${NAMESPACE:-multica-test}"
# A fresh tag every publish. Reusing multica/agent-*:test leaves the node
# running the image imported last time, because containerd keeps that tag
# and imagePullPolicy is Never.
stamp="$(date -u +%Y%m%d%H%M%S)"
api_image="multica/agent-api:${stamp}"
web_image="multica/agent-web:${stamp}"
postgres_image="${POSTGRES_IMAGE:-postgres:17}"
go_runtime="${GO_RUNTIME:-172.30.10.185:15000/common/golang:1.27.0}"
node_runtime="${NODE_RUNTIME:-172.30.10.185:15000/common/node:22-chrome}"
ingress_host="${INGRESS_HOST:-}"
ingress_class="${INGRESS_CLASS:-nginx}"

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is not installed" >&2
  exit 1
fi
if ! command -v docker >/dev/null 2>&1; then
  echo "docker is not installed" >&2
  exit 1
fi

repo="$(cd "${root}/../.." && pwd)"

load_images_on_node() {
  local tar="$1"
  echo "把镜像文件拷到节点 ${node} 并导入。文件较大，这段时间终端不再刷屏。"
  kubectl -n "${namespace}" delete pod image-loader --ignore-not-found --wait=true >/dev/null
  kubectl -n "${namespace}" apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: image-loader
  namespace: ${namespace}
spec:
  restartPolicy: Never
  nodeName: ${node}
  containers:
    - name: loader
      image: ${postgres_image}
      imagePullPolicy: IfNotPresent
      command: ["sleep", "3600"]
      securityContext:
        privileged: true
      volumeMounts:
        - name: images
          mountPath: /images
        - name: host
          mountPath: /host
  volumes:
    - name: images
      hostPath:
        path: /tmp/multica-images
        type: DirectoryOrCreate
    - name: host
      hostPath:
        path: /
EOF
  kubectl -n "${namespace}" wait --for=condition=Ready pod/image-loader --timeout=180s
  kubectl -n "${namespace}" cp "${tar}" image-loader:/images/multica.tar
  kubectl -n "${namespace}" exec image-loader -- \
    chroot /host ctr -n k8s.io images import /tmp/multica-images/multica.tar
  kubectl -n "${namespace}" exec image-loader -- \
    chroot /host sh -c "ctr -n k8s.io images ls -q | while read -r name; do
      case \"\${name}\" in
        *multica/agent-api:${stamp}|*multica/agent-web:${stamp}) ;;
        *multica/agent-api:*|*multica/agent-web:*) ctr -n k8s.io images rm \"\${name}\" || true ;;
      esac
    done"
  kubectl -n "${namespace}" exec image-loader -- \
    chroot /host rm -f /tmp/multica-images/multica.tar
  kubectl -n "${namespace}" delete pod image-loader --wait=true >/dev/null
}

node="${NODE:-}"
if [ -z "${node}" ]; then
  node="$(kubectl get nodes --no-headers | awk '$2=="Ready" {print $1; exit}')"
fi
if [ -z "${node}" ]; then
  echo "没有 Ready 的节点" >&2
  exit 1
fi

echo "kubeconfig: ${KUBECONFIG}"
echo "namespace:  ${namespace}"
echo "node:       ${node}"
echo "api image:  ${api_image}"
echo "web image:  ${web_image}"

kubectl apply -f "${root}/namespace.yaml"
kubectl -n "${namespace}" delete pod build-api build-web --ignore-not-found --wait=false >/dev/null

if ! kubectl -n "${namespace}" get secret multica-postgres >/dev/null 2>&1; then
  password="$(openssl rand -hex 16)"
  database_url="postgres://multica:${password}@postgres.${namespace}.svc.cluster.local:5432/multica?sslmode=disable"
  kubectl -n "${namespace}" create secret generic multica-postgres \
    --from-literal=POSTGRES_PASSWORD="${password}" \
    --from-literal=DATABASE_URL="${database_url}"
  echo "postgres secret created"
else
  echo "postgres secret already exists"
fi

sed \
  -e "s#POSTGRES_IMAGE#${postgres_image}#g" \
  "${root}/postgres.yaml" | kubectl apply -f -
kubectl -n "${namespace}" rollout status deployment/postgres --timeout=300s

if [ "${SKIP_BUILD:-}" != "1" ]; then
  echo "在本机打镜像"
  "${repo}/docker/agent-api/build.sh"
  docker build --platform linux/amd64 -f "${repo}/docker/agent-api/Dockerfile" \
    --build-arg GO_RUNTIME="${go_runtime}" \
    -t "${api_image}" "${repo}"
  "${repo}/docker/agent-web/build.sh"
  docker build --platform linux/amd64 -f "${repo}/docker/agent-web/Dockerfile" \
    --build-arg NODE_RUNTIME="${node_runtime}" \
    -t "${web_image}" "${repo}"
  image_tar="${root}/multica-images.tar"
  echo "导出镜像文件"
  docker save -o "${image_tar}" "${api_image}" "${web_image}"
  ls -lh "${image_tar}"
  load_images_on_node "${image_tar}"
  rm -f "${image_tar}"
  docker rmi "${api_image}" "${web_image}"
  docker image prune -f
fi

# Each publish uses a new image tag. published-at also changes the pod
# template, so the running process is replaced by that image.
published_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
# The browser opens the site on a node IP. /ws is proxied to the API, so the
# API does not see that address as its own Host and rejects the upgrade
# unless the address is on the origin allowlist.
web_port="$(kubectl -n "${namespace}" get svc agent-web -o jsonpath='{.spec.ports[0].nodePort}' 2>/dev/null || true)"
web_origins="http://127.0.0.1:3000"
if [ -n "${web_port}" ]; then
  web_origins="$(kubectl get nodes -o wide --no-headers | awk -v port="${web_port}" '$6 != "" { urls = urls (urls ? "," : "") "http://" $6 ":" port } END { print urls }')"
fi
echo "更换正在运行的容器（${published_at}）"
echo "网页来源：${web_origins}"
sed \
  -e "s#API_IMAGE#${api_image}#g" \
  -e "s#NODE_NAME#${node}#g" \
  -e "s#PUBLISHED_AT#${published_at}#g" \
  -e "s#WEB_ORIGINS#${web_origins}#g" \
  "${root}/agent-api.yaml" | kubectl apply -f -
sed \
  -e "s#WEB_IMAGE#${web_image}#g" \
  -e "s#NODE_NAME#${node}#g" \
  -e "s#PUBLISHED_AT#${published_at}#g" \
  "${root}/agent-web.yaml" | kubectl apply -f -

if [ -n "${ingress_host}" ]; then
  echo "ingress:    ${ingress_host} (${ingress_class})"
  sed \
    -e "s#INGRESS_HOST#${ingress_host}#g" \
    -e "s#INGRESS_CLASS#${ingress_class}#g" \
    "${root}/ingress.yaml" | kubectl apply -f -
else
  echo "ingress:    skipped (set INGRESS_HOST to publish the site)"
fi

if ! kubectl -n "${namespace}" rollout status deployment/agent-api --timeout=600s; then
  echo "agent-api 没有就绪，最近事件：" >&2
  kubectl -n "${namespace}" describe pod -l app=agent-api | sed -n '/Events:/,$p' >&2
  exit 1
fi
if ! kubectl -n "${namespace}" rollout status deployment/agent-web --timeout=300s; then
  echo "agent-web 没有就绪，最近事件：" >&2
  kubectl -n "${namespace}" describe pod -l app=agent-web | sed -n '/Events:/,$p' >&2
  exit 1
fi
kubectl -n "${namespace}" get deploy,svc,ingress

node_port="$(kubectl -n "${namespace}" get svc agent-web -o jsonpath='{.spec.ports[0].nodePort}')"
echo
echo "网页从节点内网 IP 的 ${node_port} 端口进入："
kubectl get nodes -o wide --no-headers | awk -v port="${node_port}" '{print "http://"$6":"port"/login"}'
