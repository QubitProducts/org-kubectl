package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"

	"github.com/golang/glog"
	"github.com/pkg/errors"
	"golang.org/x/oauth2/google"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/cloudresourcemanager/v1beta1"
	"google.golang.org/api/container/v1"
)

func main() {
	ctx := context.Background()
	flag.CommandLine.Parse([]string{"-logtostderr"})

	if len(os.Args) <= 1 {
		fmt.Fprintln(os.Stderr, "org-kubectl FOLDER [kubectl args]")
		os.Exit(1)
	}

	crm, gke, err := clients(ctx)
	if err != nil {
		glog.Errorf("could not authenticate to google: %v", err)
		os.Exit(1)
	}

	cachePath := path.Join(os.Getenv("HOME"), ".kube", "cache", "org-kubectl.json")
	ancestorCache, _ := openCache(cachePath)
	projects, err := findChildProjects(ctx, crm, os.Args[1], ancestorCache)
	if err != nil {
		glog.Errorf("could not find projects: %v", err)
		os.Exit(1)
	}
	saveCache(cachePath, ancestorCache)

	for _, p := range projects {
		resp, err := gke.Projects.Zones.Clusters.List(p, "-").Context(ctx).Do()
		if err != nil {
			glog.Errorf("could not list gke clusters in %v: %v", p, err)
			os.Exit(1)
		}

		for _, c := range resp.Clusters {
			err := getClusterCredentials(ctx, p, c)
			if err != nil {
				glog.Errorf("could not get cluster credentials for %v in %v: %v", c.Name, p, err)
				os.Exit(1)
			}

			context := fmt.Sprintf("gke_%v_%v_%v", p, c.Zone, c.Name)
			err = runKubectlCmd(ctx, context, os.Args[2:len(os.Args)])
			if err != nil {
				glog.Errorf("could not run kubectl for %v in %v: %v", c.Name, p, err)
				os.Exit(1)
			}
		}
	}
}

func runKubectlCmd(ctx context.Context, context string, additionalArgs []string) error {
	args := append([]string{
		"--context",
		context,
	}, additionalArgs...)
	glog.Infof("kubectl %v", strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, "kubectl", args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err := cmd.Run()
	return errors.Wrap(err, "could not run kubectl")
}

func getClusterCredentials(ctx context.Context, project string, cluster *container.Cluster) error {
	cmd := exec.CommandContext(ctx,
		"gcloud",
		"--project",
		project,
		"container",
		"clusters",
		"get-credentials",
		cluster.Name,
		"--zone",
		cluster.Zone,
	)
	err := cmd.Run()
	return errors.Wrap(err, "could not get cluster credentials")
}

func clients(ctx context.Context) (*cloudresourcemanager.Service, *container.Service, error) {
	httpClient, err := google.DefaultClient(ctx, cloudresourcemanager.CloudPlatformReadOnlyScope)
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not authenticate to google")
	}

	crm, err := cloudresourcemanager.New(httpClient)
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not create cloudresourcemanger client")
	}

	gke, err := container.New(httpClient)
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not create gke client")
	}
	return crm, gke, nil
}

func findChildProjects(ctx context.Context, crm *cloudresourcemanager.Service, parentResourceID string, ancestorCache map[string][]string) ([]string, error) {
	projects, err := listProjects(ctx, crm)
	if err != nil {
		return nil, err
	}

	filteredProjects := []string{}
	mu := &sync.Mutex{}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	grp, ctx := errgroup.WithContext(ctx)

	glog.Infof("looking for projects with ancestors %v", parentResourceID)
	for _, p := range projects {
		p := p

		mu.Lock()
		ancestors, ok := ancestorCache[p]
		mu.Unlock()
		if !ok {
			grp.Go(func() error {
				resp, err := crm.Projects.GetAncestry(p, &cloudresourcemanager.GetAncestryRequest{}).Context(ctx).Do()
				if err != nil {
					return errors.Wrapf(err, "could not get ancestry for %v", p)
				}
				ancestors := []string{}
				for _, ancestor := range resp.Ancestor {
					ancestors = append(ancestors, ancestor.ResourceId.Id)
					glog.Infof("ancestry for %v: %v", p, ancestor.ResourceId.Id)
					if ancestor.ResourceId.Id == parentResourceID {
						mu.Lock()
						filteredProjects = append(filteredProjects, p)
						mu.Unlock()
					}
				}
				mu.Lock()
				ancestorCache[p] = ancestors
				mu.Unlock()
				return nil
			})
		} else {
			for _, ancestor := range ancestors {
				if ancestor == parentResourceID {
					mu.Lock()
					filteredProjects = append(filteredProjects, p)
					mu.Unlock()
				}
			}
		}
	}

	if err := grp.Wait(); err != nil {
		return nil, errors.Wrap(err, "could not get project ancestors")
	}
	return filteredProjects, nil
}

func listProjects(ctx context.Context, crm *cloudresourcemanager.Service) ([]string, error) {
	projects := []string{}
	err := crm.Projects.List().Context(ctx).Pages(
		ctx, func(r *cloudresourcemanager.ListProjectsResponse) error {
			for _, p := range r.Projects {
				projects = append(projects, p.ProjectId)
			}
			return nil
		})
	if err != nil {
		return nil, errors.Wrap(err, "could not list projects")
	}
	return projects, nil
}

func openCache(path string) (map[string][]string, error) {
	c := map[string][]string{}

	f, err := os.Open(path)
	if err != nil {
		return c, errors.Wrap(err, "could not open cache file")
	}
	defer f.Close()

	err = json.NewDecoder(f).Decode(&c)
	return c, errors.Wrap(err, "could not decode cache")
}

func saveCache(path string, cache map[string][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return errors.Wrap(err, "could not create cache file")
	}
	defer f.Close()

	err = json.NewEncoder(f).Encode(&cache)
	return errors.Wrap(err, "could not encode cache")
}
