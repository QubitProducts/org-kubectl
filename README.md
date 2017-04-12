# org-kubectl

Run kubernetes commands across every GKE cluster in an organisation or folder.

## Usage examples

To list all pods in projects in the folder with id 50531938921

```
▶ org-kubectl 50531938921 get po
I0412 22:28:58.136606   12745 main.go:131] looking for projects with ancestors 50531938921
I0412 22:28:59.754816   12745 main.go:75] kubectl --context gke_project-name_us-central1-c_cluster-1 get po
NAME                          READY     STATUS    RESTARTS   AGE
prometheus-4120551589-mj927   1/1       Running   0          23h
```
