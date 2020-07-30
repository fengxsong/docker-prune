package main

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
)

func main() {
	filterF := pflag.StringSliceP("filter", "f", []string{}, "Provide filter values (e.g. 'label=<key>=<value>')")
	intervalF := pflag.Duration("interval", 24*time.Hour, "Cleaning job interval")
	allF := pflag.BoolP("all", "a", false, "Remove all unused images not just dangling ones")
	pflag.Parse()

	logger := logrus.New()

	var (
		args = filters.NewArgs()
		err  error
	)
	for _, s := range *filterF {
		args, err = filters.ParseFlag(s, args)
		if err != nil {
			logger.Fatalf("Failed to parse filter argument: %v", err)
		}
	}

	cli, err := client.NewEnvClient()
	if err != nil {
		logger.Fatalf("Failed to create Docker client: %v", err)
	}

	ticker := time.NewTicker(*intervalF)
	defer func() {
		ticker.Stop()
		cli.Close()
	}()

	queue := make(chan struct{}, 1)
	queue <- struct{}{}

	for {
		select {
		case <-ticker.C:
			queue <- struct{}{}
		case <-queue:
			logger.Info("Start cleaning up unused data")
			ctx, cancel := context.WithTimeout(context.Background(), *intervalF-time.Second)
			defer cancel()
			errCh := make(chan error)
			go func() {
				errCh <- runPrune(ctx, logger, cli, *allF, args)
			}()
			select {
			case <-ctx.Done():
				logger.Warn(ctx.Err().Error())
			case err := <-errCh:
				if err != nil {
					logger.Error("Error occur: %v", err)
				} else {
					logger.Info("Finished cleaning")
				}
			}
		}
	}
}

func runPrune(ctx context.Context, logger *logrus.Logger, cli client.APIClient, all bool, pruneFilter filters.Args) error {
	pruneFuncs := []func(context.Context, client.APIClient, bool, filters.Args) (uint64, string, error){
		pruneContainers,
		pruneImages,
		pruneNetworks,
		pruneVolumes,
	}
	var total uint64
	for i := range pruneFuncs {
		spaceReclaimed, output, err := pruneFuncs[i](ctx, cli, all, pruneFilter)
		if err != nil {
			return err
		}
		total += spaceReclaimed
		logger.Info(output)
	}
	logger.Infof("Total reclaimed space: %d", total)
	return nil
}

func pruneContainers(ctx context.Context, cli client.APIClient, all bool, pruneFilter filters.Args) (uint64, string, error) {
	report, err := cli.ContainersPrune(ctx, pruneFilter)
	if err != nil {
		return 0, "", err
	}
	return report.SpaceReclaimed, fmt.Sprintf("Deleted Containers: %d, Reclaimed Space: %d", len(report.ContainersDeleted), report.SpaceReclaimed), nil
}

func pruneNetworks(ctx context.Context, cli client.APIClient, all bool, pruneFilter filters.Args) (uint64, string, error) {
	report, err := cli.NetworksPrune(ctx, pruneFilter)
	if err != nil {
		return 0, "", err
	}
	return 0, fmt.Sprintf("Deleted Networks: %d", len(report.NetworksDeleted)), nil
}

func pruneVolumes(ctx context.Context, cli client.APIClient, all bool, pruneFilter filters.Args) (uint64, string, error) {
	report, err := cli.VolumesPrune(ctx, pruneFilter)
	if err != nil {
		return 0, "", err
	}
	return report.SpaceReclaimed, fmt.Sprintf("Deleted Volumes: %d, Reclaimed Space: %d", len(report.VolumesDeleted), report.SpaceReclaimed), nil
}

func pruneImages(ctx context.Context, cli client.APIClient, all bool, pruneFilter filters.Args) (uint64, string, error) {
	newArgs := cloneArgs(pruneFilter)
	newArgs.Add("dangling", fmt.Sprintf("%v", !all))
	report, err := cli.ImagesPrune(ctx, newArgs)
	if err != nil {
		return 0, "", err
	}
	return report.SpaceReclaimed, fmt.Sprintf("Deleted Images: %d, Reclaimed Space: %d", len(report.ImagesDeleted), report.SpaceReclaimed), nil
}

// In older versions of docker/client, filters.Args struct does not implement Clone() function
// or we could simplely copy the value of the pointer?
func cloneArgs(pruneFilter filters.Args) filters.Args {
	s, _ := filters.ToParam(pruneFilter)
	newArgs, _ := filters.FromParam(s)
	return newArgs
}
