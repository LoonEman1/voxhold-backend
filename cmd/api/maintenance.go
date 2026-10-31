package main

import (
	"context"
	"log"
	"time"
)

type maintenanceTask struct {
	name    string
	timeout time.Duration
	run     func(context.Context) error
}

func runStorageMaintenance(parent context.Context, tasks ...maintenanceTask) {
	for _, task := range tasks {
		if parent.Err() != nil {
			return
		}
		ctx, cancel := context.WithTimeout(parent, task.timeout)
		err := task.run(ctx)
		cancel()
		if err != nil && parent.Err() == nil {
			log.Printf("%s cleanup: %v", task.name, err)
		}
	}
}
