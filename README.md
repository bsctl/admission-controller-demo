# Kubernetes Admission Controller Demo
This repo contains a demo server which implements a [Mutating Admission Controller](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/) for Kubernetes.

The Mutating Admission Controller demo is inspired by the embedded [Pod Node Selector](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#podnodeselector) admission controller which control what node selectors may be used within a given namespace.

For example, we may want all pods deployed in the production namespace to have specific node selector(s). The node selector(s) are then used by the default scheduler to assign the pods to a dedicated set of nodes reserved for production.

Unlike the embedded Pod Node Selector controller, this custom admission controller does not requires the APIs server to be reloaded in case of changes in the configuration.

Please, see the [A guide to Kubernetes policy controllers](doc/guide-kubernetes-policy-controllers.md) for the rationale behind this demo.

## Prerequisites
To run this demo, please, ensure that:

 * the Kubernetes cluster is at least as new as v1.14
 * the admissionregistration.k8s.io/v1beta1 API is enabled:
    
        $ kubectl api-versions | grep admissionregistration.k8s.io/v1beta1
        admissionregistration.k8s.io/v1beta1
 
 * the ``MutatingAdmissionWebhook`` and the ``ValidatingAdmissionWebhook`` admission controllers are enabled in the APIs Server configuration:
 
        --enable-admission-plugins=MutatingAdmissionWebhook,ValidatingAdmissionWebhook, ...
 
  See Kubernetes documentation for how to enable Admission Controllers and for a recommended set of controllers to be enabled.

## Build the image
Clone this repo on a local machine where ``kubectl`` is configured against the target Kubernetes cluster. The image is built from the provided [Dockerfile](Dockerfile):

    docker build -t webhook:v0.0.1 .

Once the image is built, push it on your preferred repository.

## Deploy
A deployment [script](deployment/deploy.sh) is provided. The script uses the Kubernetes Certificate Signing Request (CSR) to a generate a certificate and key signed by the Kubernetes CA itself. This certificate and key are required to secure the communication between the webhook server and the APIs server. Make sure you have the permissions to create and approve CSR in your Kubernetes cluster.

Also, before to attempt the script, please update all relevant parameters according to your environment and permissions:

    service="webhook-server"
    namespace="default"
    repository=""
    image="webhook"
    version="v0.0.1"

Run the script. The script ends with a bounch of manifest files and apply them to Kubernetes:


    $ cd deployment
    $ ./deploy.sh
    creating certs in tmpdir /tmp/tmp.iYxAX9oyU8 
    Generating RSA private key, 2048 bit long modulus
    ...
    certificatesigningrequest.certificates.k8s.io/webhook-server created
    certificatesigningrequest.certificates.k8s.io/webhook-server approved
    mutatingwebhookconfiguration.admissionregistration.k8s.io/webhook-server created
    certificatesigningrequest.certificates.k8s.io/webhook-server configured
    deployment.extensions/webhook-server created
    configmap/webhook-server created
    service/webhook-server created
    secret/webhook-server created


Verify the webhook server pod in the selected namespace is running:

    $ kubectl get pods -n default

    NAME                             READY   STATUS    RESTARTS   AGE
    webhook-server-bd7998fdb-mfwjw   1/1     Running   0          16m

## Operate
The business logic of webhook is controlled by the ``rules.json`` configuration file passed to the webhook as Config Map. By default, the configuration file looks like:

```json
{
    "defaultselector": "fruit=banana",
    "rules": {
    "development":  "env=development",
    "production":   "env=production"
    }
}
```

It defines a set of rules: each rule contains a namespace and the list of label(s) to be assigned as node selector(s) to the pods.

In the default configuration, all the pods deployed into ``production`` namespace will use the label ``env=production`` as node selector and all the pods deployed into ``development`` namespace will use the label ``env=development``.

In addition, a default selector ``fruit=banana`` is specified for all the other namespaces not listed above.

### Create the namespaces
To check the webook, let's to create first the namespaces

    $ kubectl create ns development
    $ kubectl create ns production

### Deploy pods
Deploy pods into namespaces:

    $ kubectl run dev \
        --image=nginx:latest \
        --port=80 \
        --generator=run-pod/v1 \
        --namespace=development

    $ kubectl run prod \
        --image=nginx:latest \
        --port=80 \
        --generator=run-pod/v1 \
        --namespace=production

Inspect the pod just created:

    $ kubectl get pods -n development
    NAME   READY   STATUS    RESTARTS   AGE
    dev    0/1     Pending   0          3m32s   

    $ kubectl get pods -n production
    NAME   READY   STATUS    RESTARTS   AGE
    prod   0/1     Pending   0          4m51s

A closer look to the pods will show us the reason for failing.

    $ kubectl describe pod dev -n development

    ...
    Events:
    Type     Reason            Age                 From               Message
    ----     ------            ----                ----               -------
    Warning  FailedScheduling  30s (x10 over 3m)   default-scheduler  0/1 nodes are available: 1 node(s) didn't match node selector.

So there are no nodes matching the applyed node selectors:

    $ kubectl get pod dev -o json -n development
        ...
        "nodeSelector": {
            "env": "development"
        },

    $ kubectl get pod prod -o json -n production
        ...
        "nodeSelector": {
            "env": "production"
        },

### Label the nodes
In order to get those pods running on the assigned nodes, we need to label nodes according to the rules above

    $ kubectl get nodes
    NAME     STATUS   ROLES    AGE    VERSION
    cmp      Ready    master   12d    v1.14.1
    worker01 Ready    worker   12d    v1.14.1
    worker02 Ready    worker   12d    v1.14.1
    worker03 Ready    worker   12d    v1.14.1

    $ kubectl label node worker01 -l env=development
    $ kubectl label node worker02 -l env=production
    $ kubectl label node worker03 -l env=production

And check the pods again

    $ kubectl get pods -o wide -n development
    NAME   READY   STATUS    RESTARTS   AGE   IP            NODE
    dev    1/1     Running   0          19m   10.38.1.123   worker01  

    $ kubectl get pods -o wide -n production
    NAME   READY   STATUS    RESTARTS   AGE   IP            NODE
    prod   1/1     Running   0          19m   10.38.2.141   worker02  

Now all pods are running on nodes according to their namespaces.


### Update the webhook rules
We can specify multiple labels as node selector. Modify the configuration rules by editing the Config Map

    $ kubectl -n default edit cm

so that the ``rules.json`` configuration file looks like the following:

```json
{
    "defaultselector": "fruit=banana",
    "rules": {
    "development":  "env=development",
    "production":   "env=production",
    "staging":      "env=staging"
    }
}
```

To have the configuration reloaded, kill the webhook pod and wait a new one is created by the deployment controller

    $ kubectl -n default get pods
    NAME                             READY   STATUS    RESTARTS   AGE
    webhook-server-bd7998fdb-7z2lh   1/1     Running   0          10m

    $ kubectl -n default delete pod webhook-server-bd7998fdb-7z2lh 

    $ kubectl -n default get pods
    NAME                             READY   STATUS    RESTARTS   AGE
    webhook-server-bd7998fdb-7z2lh   1/1     Running   0          13s

Label one of the nodes with the new label

    $ kubectl label node worker03 -l env=staging

Create the new ``staging`` namespace

    $ kubectl create namespace staging

and let's to deploy a new pod into the namespace

    $ kubectl run stage \
        --image=nginx:latest \
        --port=80 \
        --generator=run-pod/v1 \
        --namespace=staging

The new pod in the ``staging`` namespace will use the label ``env=staging`` as node selector

    $ kubectl get pod stage -o json -n staging
        ...
        "nodeSelector": {
            "env": "staging"
        },

and it will be running on the node ``worker03`` we just labeled before

    $ kubectl get pods -o wide -n staging
    NAME   READY   STATUS    RESTARTS   AGE   IP           NODE
    stage  1/1     Running   0          12s   10.38.3.4    worker03



