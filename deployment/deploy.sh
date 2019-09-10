#!/bin/bash

# This script uses Kubernetes CertificateSigningRequest (CSR) to a generate a
# certificate signed by the Kubernetes CA itself.
# It is suitable only for use with Admission Controller webhooks running inside the cluster.
# This script requires permissions to create and approve CSR. 

# Check if OpenSSL is installed
if [ ! -x "$(command -v openssl)" ]; then
    echo "Error: openssl not found"
    exit 1
fi

service="webhook-server"
namespace="default"
repository=""
image="webhook"
version="v0.0.1"

tmpdir=$(mktemp -d)
echo "creating certs in tmpdir ${tmpdir} "

cat <<EOF >> ${tmpdir}/csr.conf
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[ v3_req ]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names
[alt_names]
DNS.1 = ${service}
DNS.2 = ${service}.${namespace}
DNS.3 = ${service}.${namespace}.svc
EOF

openssl genrsa -out ${tmpdir}/tls.key 2048
openssl req -new -key ${tmpdir}/tls.key -subj "/CN=${service}.${namespace}.svc" -out ${tmpdir}/${service}.csr -config ${tmpdir}/csr.conf

# Clean any previously created CSR for the same service.
kubectl delete csr ${csrName} 2>/dev/null || true

# Create a new CSR file.
cat <<EOF > ${service}-csr.yaml
apiVersion: certificates.k8s.io/v1beta1
kind: CertificateSigningRequest
metadata:
  name: ${service}
spec:
  groups:
  - system:authenticated
  request: $(cat ${tmpdir}/${service}.csr | base64 | tr -d '\n')
  usages:
  - digital signature
  - key encipherment
  - server auth
EOF

# Create the CSR
kubectl apply -f ${service}-csr.yaml

# Approve and fetch the signed certificate
kubectl certificate approve ${service}
kubectl get csr ${service} -o jsonpath='{.status.certificate}' | base64 --decode > ${tmpdir}/tls.crt

# Create the Secret file
cat <<EOF > ${service}-tls.yaml
apiVersion: v1
data:
  tls.crt: $(cat ${tmpdir}/tls.crt | base64 | tr -d '\n')
  tls.key: $(cat ${tmpdir}/tls.key | base64 | tr -d '\n')
kind: Secret
metadata:
  name: ${service}
  namespace: ${namespace}
type: kubernetes.io/tls
EOF

# Create the Mutating Webhook Configuration file
cat <<EOF > ${service}-admission.yaml
apiVersion: admissionregistration.k8s.io/v1beta1
kind: MutatingWebhookConfiguration
metadata:
  name: ${service}
webhooks:
- name: ${service}.${namespace}.svc
  clientConfig:
    caBundle: $(kubectl get configmap -n kube-system extension-apiserver-authentication -o=jsonpath='{.data.client-ca-file}' | base64 | tr -d '\n')
    service:
      name: ${service}
      namespace: ${namespace}
      path: /mutate
  timeoutSeconds: 30
  failurePolicy: Ignore
  namespaceSelector:
    matchExpressions:
    - key: role
      operator: NotIn 
      values: ["system"]
  rules:
  - apiGroups: [""]
    apiVersions: ["v1"]
    operations: ["CREATE", "UPDATE"] 
    resources: ["pods"]
    scope: Namespaced
EOF

# Create the Service file
cat <<EOF > ${service}-svc.yaml
apiVersion: v1
kind: Service
metadata:
  name: ${service}
  namespace: ${namespace}
spec:
  ports:
  - port: 443
    protocol: TCP
    targetPort: webhook-api
  selector:
    app: ${service}
  type: ClusterIP
EOF

# Create the Deployment file
cat <<EOF > ${service}-deploy.yaml
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: ${service}
  namespace: ${namespace}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ${service}
  strategy:
  template:
    metadata:
      labels:
        app: ${service}
    spec:
      containers:
      - name: webhook
        image: ${repository}${image}:${version}
        command:
        - webhook
        - --debug
        ports:
        - containerPort: 8443
          name: webhook-api
          protocol: TCP
        resources:
          requests:
            cpu: 100m
            memory: 512Mi
        volumeMounts:
        - mountPath: /opt/certs
          name: certs
          readOnly: true
        - mountPath: /opt/config
          name: rules
          readOnly: true
      volumes:
      - name: certs
        secret:
          secretName: ${service}
      - name: rules
        configMap:
          name: ${service}
EOF

# Create the Rules file
cat <<EOF > ${service}-rules.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: ${service}
  namespace: ${namespace}
data:
  rules.json: |
    {
      "defaultselector": "fruit=banana",
      "rules": {
      "development":  "env=development",
      "production":   "env=production"
      }
    }
EOF

# Create all the resources
kubectl apply -f .
