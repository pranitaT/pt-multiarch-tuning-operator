package common

const SingletonResourceObjectName = "cluster"

type Plugin int

const (
	// MainPlugin checks the core pod placement resources.
	NodeAffinityScoringPluginName Plugin = iota
	// ENoExecPlugin checks the ENoExecEvent resources.
	ExecFormatErrorMonitorPluginName
	// CELArchitecturePlacementPluginName is the plugin for CEL-based architecture placement.
	CELArchitecturePlacementPluginName
)
