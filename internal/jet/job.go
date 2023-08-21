package jet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/hazelcast/hazelcast-go-client"
	"github.com/hazelcast/hazelcast-go-client/types"

	"github.com/hazelcast/hazelcast-commandline-client/internal"
	"github.com/hazelcast/hazelcast-commandline-client/internal/cluster"
	"github.com/hazelcast/hazelcast-commandline-client/internal/log"
	"github.com/hazelcast/hazelcast-commandline-client/internal/proto/codec"
	"github.com/hazelcast/hazelcast-commandline-client/internal/proto/codec/control"
)

type spinner interface {
	SetProgress(progress float32)
}

type BinaryReader interface {
	Hash() ([]byte, error)
	Reader() (io.ReadCloser, error)
	FileName() string
	PartCount(batchSize int) (int, error)
}

type Jet struct {
	ci *hazelcast.ClientInternal
	sp spinner
	lg log.Logger
}

func New(ci *hazelcast.ClientInternal, sp spinner, lg log.Logger) *Jet {
	return &Jet{
		ci: ci,
		sp: sp,
		lg: lg,
	}
}

func (j Jet) SubmitYamlJob(ctx context.Context, yaml string, jobm map[interface{}]interface{}, jobName string, snapshot string, args []string) error {
	j.sp.SetProgress(0)

	// note: what about 'EncodeJetUploadJobMetaDataRequest' codec thing?
	//   esp. what about the 'snapshot' string thing?
	//   where does the job UID come from?
	// get the job unique ID
	// upload the resources
	// upload the yaml file

	// TODO
	//  - get the job name from the yaml file, overwrite with jobName if not empty
	//  - get the snapshot from the yaml file, overwrite with snapshot if not empty
	//  - parse the yaml to deal with the resources = we may not want a binary reader?

	if jobName != "" {
		jobm["name"] = jobName
	}

	if snapshot != "" {
		jobm["snapshot"] = snapshot
	}

	// or, should we have a job:config thing with name, snapshot, and way more?

	// get a new job ID
	//await using var jetIdGenerator = await _client.GetFlakeIdGeneratorAsync("__jet.ids");
	//return await jetIdGenerator.GetNewIdAsync().CfAwait();
	idGenerator, err := j.ci.Client().GetFlakeIDGenerator(ctx, "__jet.ids")
	if err != nil {
		return fmt.Errorf("failed to get job id generator: %w", err)
	}
	jobId, err := idGenerator.NewID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get job id: %w", err)
	}

	// upload resources
	res, exists := jobm["resources"]
	if exists {
		resl := res.([]interface{})
		for _, r := range resl {
			rm := r.(map[interface{}]interface{})
			path := rm["path"].(string)
			err = uploadDirectoryResource(ctx, jobId, path)
			if err != nil {
				return fmt.Errorf("failed to upload resource: %w", err)
			}
		}
	}

	dryRun := false // TODO should be a flag?
	submitRq := codec.EncodeJetSubmitYamlJobRequest(jobId, dryRun, yaml)
	mem, err := cluster.RandomMember(ctx, j.ci)
	if err != nil {
		return err
	}
	if _, err = j.ci.InvokeOnMember(ctx, submitRq, mem, nil); err != nil {
		return err
	}
	if err != nil {
		return fmt.Errorf("uploading job definition: %w", err)
	}

	j.sp.SetProgress(1)
	return nil
}

func uploadDirectoryResource(ctx context.Context, jobId int64, path string) error {
	/*
		// prefix is f. for file, c. for class (see job repository)
		var id = Path.GetFileName(path);
		var key = $"f.dotnet-{id}-";
		var rnd = Guid.NewGuid().ToString("N").Substring(0, 9);
		var zipPath = Path.Combine(Path.GetTempPath(), rnd + "-" + Path.GetFileName(id) + ".zip");

		using (var zipFile = new ZipFile())
		{
		    zipFile.CompressionMethod = CompressionMethod.Deflate;
		    zipFile.AddDirectory(path);
		    zipFile.Save(zipPath);
		}

		try
		{
		    var resourcesMapName = $"__jet.resources.{JobIdToString(jobId)}";
		    await using var jobResources = await _client.GetMapAsync<string, byte[]>(resourcesMapName).CfAwait();
		    await new MapOutputStream(jobResources, key).WriteFileAsync(zipPath).CfAwait();
		}
		finally
		{
		    File.Delete(zipPath);
		}
	*/
	return nil //fmt.Errorf("not implemented")
}

func (j Jet) SubmitJob(ctx context.Context, path, jobName, className, snapshot string, args []string, br BinaryReader) error {
	_, fn := filepath.Split(path)
	fn = br.FileName()
	j.sp.SetProgress(0)
	hashBin, err := br.Hash()
	if err != nil {
		return err
	}
	hash := fmt.Sprintf("%x", hashBin)
	sid := types.NewUUID()
	mrReq := codec.EncodeJetUploadJobMetaDataRequest(sid, false, fn, hash, snapshot, jobName, className, args)
	mem, err := cluster.RandomMember(ctx, j.ci)
	if err != nil {
		return err
	}
	if _, err = j.ci.InvokeOnMember(ctx, mrReq, mem, nil); err != nil {
		return err
	}
	if err != nil {
		return fmt.Errorf("uploading job metadata: %w", err)
	}
	pc, err := br.PartCount(defaultBatchSize)
	if err != nil {
		return err
	}
	f, err := br.Reader()
	if err != nil {
		return err
	}
	// TODO: decide whether to close or not to close the reader
	defer f.Close()
	j.lg.Info("Sending %s in %d batch(es)", path, pc)
	conn := j.ci.ConnectionManager().RandomConnection()
	if conn == nil {
		return errors.New("no connection to the server")
	}
	// see: https://hazelcast.atlassian.net/browse/HZ-2492
	sv := conn.ServerVersion()
	workaround := internal.CheckVersion(sv, "=", "5.3.0")
	bb := newBatch(f, defaultBatchSize)
	for i := int32(0); i < int32(pc); i++ {
		bin, hashBin, err := bb.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("sending the job: %w", err)
		}
		part := i + 1
		hash = fmt.Sprintf("%x", hashBin)
		if workaround && hash[0] == '0' {
			hash = strings.TrimLeft(hash, "0")
		}
		mrReq = codec.EncodeJetUploadJobMultipartRequest(sid, part, int32(pc), bin, int32(len(bin)), hash)
		if _, err := j.ci.InvokeOnMember(ctx, mrReq, mem, nil); err != nil {
			return fmt.Errorf("uploading part %d: %w", part, err)
		}
		j.sp.SetProgress(float32(part) / float32(pc))
	}
	j.sp.SetProgress(1)
	return nil
}

func (j Jet) GetJobList(ctx context.Context) ([]control.JobAndSqlSummary, error) {
	req := codec.EncodeJetGetJobAndSqlSummaryListRequest()
	resp, err := j.ci.InvokeOnRandomTarget(ctx, req, nil)
	if err != nil {
		return nil, err
	}
	ls := codec.DecodeJetGetJobAndSqlSummaryListResponse(resp)
	return ls, nil
}

func (j Jet) TerminateJob(ctx context.Context, jobID int64, terminateMode int32) error {
	req := codec.EncodeJetTerminateJobRequest(jobID, terminateMode, types.UUID{})
	if _, err := j.ci.InvokeOnRandomTarget(ctx, req, nil); err != nil {
		return err
	}
	return nil
}

func (j Jet) ExportSnapshot(ctx context.Context, jobID int64, name string, cancel bool) error {
	req := codec.EncodeJetExportSnapshotRequest(jobID, name, cancel)
	if _, err := j.ci.InvokeOnRandomTarget(ctx, req, nil); err != nil {
		return err
	}
	return nil
}

func (j Jet) ResumeJob(ctx context.Context, jobID int64) error {
	req := codec.EncodeJetResumeJobRequest(jobID)
	if _, err := j.ci.InvokeOnRandomTarget(ctx, req, nil); err != nil {
		return err
	}
	return nil
}

func EnsureJobState(jobs []control.JobAndSqlSummary, jobNameOrID string, state int32) (bool, error) {
	for _, j := range jobs {
		if j.NameOrId == jobNameOrID {
			if j.Status == state {
				return true, nil
			}
			if j.Status == JobStatusFailed {
				return false, ErrJobFailed
			}
			return false, nil
		}
	}
	return false, ErrJobNotFound
}
