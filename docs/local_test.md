# Test Locally

## Run and check cluster

1. Create and run a cluster if it is not running yet.

cd helm-charts-priv
task local-setup

2. Verify that the cluster is running.

Run k9s, go to `:pods`. All pods must have a status of "Running".
It may take some time before they are all ready. The possible issues may be insufficient RAM and/or CPU cores. In this case, increase the limits in Docker settings.

3. In k9s, go to `:pods`, then open pod `kubernetes-graphql-gateway-...`.

Open container `kubernetes-graphql-gateway-gateway` to see the logs.
The logs must contain more than a single line (with "Starting server...").
If you see only this single line, the problem might be in the container called "kubernetes-graphql-gateway-listener".

Note the `IMAGE` column, corresponding to the two `kubernetes-...` container. It contains the name and the currently used version of the build, i.e.
`ghcr.io/openmfp/kubernetes-graphql-gateway:v0.75.1`

4. Build the Docker image:
```
task docker
```

5. Tag the newly built image with the version you want to test:
```
docker tag ghcr.io/openmfp/kubernetes-graphql-gateway:latest ghcr.io/openmfp/kubernetes-graphql-gateway:v0.75.1
```
Use the name you and version got from the `IMAGE` column on step 3. Leave the version number unchanged.

6. Check your cluster name:
```
kind get clusters
```
In this example, the cluster name is `openmfp`.

7. Load the new image into your kind cluster:
```
kind load docker-image ghcr.io/openmfp/kubernetes-graphql-gateway:v0.75.1 -n openmfp
```
The argument `-n openmfp` is to change the default value of the cluster name, which is `kind`.

8. In k9s, go to `:pods` and delete the pod (not the container) called `kubernetes-graphql-gateway-...`.

Kubernetes will immediately recreate the pod -- but this time it will use the new version of the build.