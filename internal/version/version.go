package version

// Version is the application build version. Override at build time with
// -ldflags "-X .../internal/version.Version=x.y.z".
var Version = "dev"
