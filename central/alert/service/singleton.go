package service

import (
	"sync"

	"github.com/stackrox/rox/central/alert/datastore"
	notifierProcessor "github.com/stackrox/rox/central/notifier/processor"
)

var (
	once         sync.Once
	soleInstance Service
)

func initialize() {
	soleInstance = New(datastore.Singleton(), notifierProcessor.Singleton())
}

// Singleton returns the sole instance of the gRPC Server Service for handling CRUD use cases for Alert objects.
func Singleton() Service {
	once.Do(initialize)
	return soleInstance
}
