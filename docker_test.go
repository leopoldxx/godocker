package godocker

import (
	"context"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"
)

func cloneRepo() string {
	return "./docker-build"
}

func TestDockerbuild(t *testing.T) {
	repoLocalPath := cloneRepo()
	defer os.RemoveAll(repoLocalPath)

	localRegistry := "127.0.0.1"

	docker, _ := NewClient(Configs{
		Host:     "tcp://127.0.0.1:2376",
		Registry: localRegistry,
	})

	if err := docker.Build(context.TODO(), repoLocalPath, localRegistry+"/public/build-test:master", map[string]*string{}); err != nil {
		t.Fatal("build docker image failed:", err)
	}

	if err := docker.Push(context.TODO(), localRegistry+"/public/build-test:master"); err != nil {
		t.Fatal("push docker image failed:", err)
	}

}
func TestDockerList(t *testing.T) {
	docker, _ := NewClient(Configs{
		Host: "tcp://127.0.0.1:2376",
	})

	list, err := docker.List(context.TODO(), map[string]string{})
	if err != nil {
		t.Fatal("list docker images failed:", err)
	}
	for _, image := range list {
		t.Logf("%+v", image)
	}
}
func TestDockerTag(t *testing.T) {
	docker, _ := NewClient(Configs{
		Host: "tcp://127.0.0.1:2376",
	})

	rand.Seed(time.Now().Unix())
	newRepo := "newRepoHost.com/ubuntu"
	newTag := strconv.Itoa(rand.Intn(1024))
	err := docker.Tag(context.TODO(), "ubuntu:16.04", newRepo+":"+newTag)
	if err != nil {
		t.Fatal("tag docker images failed:", err)
	}
	list, err := docker.List(context.TODO(), map[string]string{})
	if err != nil {
		t.Fatal("list docker images failed:", err)
	}
	for _, image := range list {
		t.Logf("%+v", image)
	}
	err = docker.Rmi(context.TODO(), newRepo+":"+newTag)
	if err != nil {
		t.Fatal("remove docker images failed:", err)
	}
}
