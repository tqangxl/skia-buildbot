/*
	Pulls data from multiple sources and funnels into InfluxDB.
*/

package main

import (
	"flag"
	"path"
	"time"

	"github.com/golang/glog"
	influxdb "github.com/influxdb/influxdb/client"
	"skia.googlesource.com/buildbot.git/datahopper/go/autoroll_ingest"
	"skia.googlesource.com/buildbot.git/go/buildbot"
	"skia.googlesource.com/buildbot.git/go/common"
	"skia.googlesource.com/buildbot.git/go/database"
	"skia.googlesource.com/buildbot.git/go/gitinfo"
	"skia.googlesource.com/buildbot.git/go/metadata"
)

const (
	INFLUXDB_NAME_METADATA_KEY     = "influxdb_name"
	INFLUXDB_PASSWORD_METADATA_KEY = "influxdb_password"
	SKIA_REPO                      = "https://skia.googlesource.com/skia"
)

// flags
var (
	graphiteServer   = flag.String("graphite_server", "localhost:2003", "Where is Graphite metrics ingestion server running.")
	useMetadata      = flag.Bool("use_metadata", true, "Load sensitive values from metadata not from flags.")
	influxDbHost     = flag.String("influxdb_host", "localhost:8086", "The InfluxDB hostname.")
	influxDbName     = flag.String("influxdb_name", "root", "The InfluxDB username.")
	influxDbPassword = flag.String("influxdb_password", "root", "The InfluxDB password.")
	influxDbDatabase = flag.String("influxdb_database", "", "The InfluxDB database.")
	workdir          = flag.String("workdir", ".", "Working directory used by data processors.")
	testing          = flag.Bool("testing", false, "Whether we're running in non-production, testing mode.")
)

func main() {
	common.InitWithMetrics("datahopper", *graphiteServer)

	// Prepare the InfluxDB credentials. Load from metadata if appropriate.
	if *useMetadata {
		*influxDbName = metadata.MustGet(INFLUXDB_NAME_METADATA_KEY)
		*influxDbPassword = metadata.MustGet(INFLUXDB_PASSWORD_METADATA_KEY)
	}
	dbClient, err := influxdb.New(&influxdb.ClientConfig{
		Host:       *influxDbHost,
		Username:   *influxDbName,
		Password:   *influxDbPassword,
		Database:   *influxDbDatabase,
		HttpClient: nil,
		IsSecure:   false,
		IsUDP:      false,
	})
	if err != nil {
		glog.Fatalf("Failed to initialize InfluxDB client: %s", err)
	}

	// Data generation goroutines.
	go autoroll_ingest.LoadAutoRollData(dbClient, *workdir)
	go func() {
		// Initialize the buildbot database.
		var conf *database.DatabaseConfig
		if *testing {
			conf = buildbot.LocalMySQLTestDatabaseConfig("test_user", "")
		} else {
			conf = buildbot.ProdDatabaseConfig(false)
		}
		if err := buildbot.InitDB(conf); err != nil {
			glog.Fatal(err)
		}
		// Create the Git repo.
		skiaRepo, err := gitinfo.CloneOrUpdate(SKIA_REPO, path.Join(*workdir, "buildbot_git", "skia"), true)
		if err != nil {
			glog.Fatal(err)
		}
		// Ingest data in a loop.
		for _ = range time.Tick(30 * time.Second) {
			skiaRepo.Update(true, true)
			glog.Info("Ingesting builds.")
			if err := buildbot.IngestNewBuilds(skiaRepo); err != nil {
				glog.Errorf("Failed to ingest new builds: %v", err)
			}
		}
	}()

	// Wait while the above goroutines generate data.
	select {}
}