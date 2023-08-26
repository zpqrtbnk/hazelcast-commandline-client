package jet

import (
	"archive/zip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hazelcast/hazelcast-go-client"
	"github.com/hazelcast/hazelcast-go-client/types"

	"github.com/hazelcast/hazelcast-commandline-client/internal"
	"github.com/hazelcast/hazelcast-commandline-client/internal/cluster"
	"github.com/hazelcast/hazelcast-commandline-client/internal/log"
	"github.com/hazelcast/hazelcast-commandline-client/internal/proto/codec"
	"github.com/hazelcast/hazelcast-commandline-client/internal/proto/codec/control"

	proto "github.com/hazelcast/hazelcast-go-client"
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
			resourceId := rm["id"].(string)
			path := rm["path"].(string)
			fmt.Printf("upload resource %s DIRECTORY %s\n", resourceId, path)
			err = uploadDirectoryResource(ctx, j.ci.Client(), jobId, resourceId, path)
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

func uploadDirectoryResource(ctx context.Context, client *hazelcast.Client, jobId int64, resourceId string, path string) error {

	key := "f." + resourceId
	rnd := strings.Replace(types.NewUUID().String(), "-", "", -1)[:9]
	_, filename := filepath.Split(path)
	zipPath := filepath.Join(os.TempDir(), rnd+"-"+filename+".zip")

	err := Zip(zipPath, path)
	if err != nil {
		return fmt.Errorf("failed to zip directory (failed to zip) %w", err)
	}

	resourcesMapName := "__jet.resources." + JobIdToString(jobId)
	//fmt.Println("  jobId: ", jobId, " -> resources map: ", resourcesMapName)
	jobResources, err := client.GetMap(ctx, resourcesMapName)
	if err != nil {
		return fmt.Errorf("failed to get resources map %s %w", resourcesMapName, err)
	}
	err = Upload(ctx, jobResources, key, zipPath)
	os.Remove(zipPath)
	if err != nil {
		return fmt.Errorf("failed to zip directory (failed to zip) %w", err)
	}

	return nil
}

func JobIdToString(jobId int64) string {
	buf := []rune("0000-0000-0000-0000")
	hexStr := fmt.Sprintf("%x", jobId)
	j := 18
	for i := len(hexStr) - 1; i >= 0; i-- {
		buf[j] = rune(hexStr[i])
		if j == 15 || j == 10 || j == 5 {
			j -= 1
		}
		j -= 1
	}
	return string(buf)
}

// https://stackoverflow.com/questions/37869793/how-do-i-zip-a-directory-containing-sub-directories-or-files-in-golang
func Zip(zipPath string, sourcePath string) error {

	sourcePath = strings.ReplaceAll(sourcePath, "\\", "/") // normalize

	file, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("failed to zip directory (failed to create zip file) %w", err)
	}
	defer file.Close()

	w := zip.NewWriter(file) // FIXME set method to Deflate, how?
	defer w.Close()

	walker := func(path string, info os.FileInfo, err error) error {
		//fmt.Printf("Crawling: %#v\n", path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil // FIXME but, recursive?!
		}
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("failed to zip directory (failed to open path) %w", err)
		}
		defer file.Close()

		// Ensure that `path` is not absolute; it should not start with "/",
		// ie that it is a zip-root relative path !
		pathInZip := strings.ReplaceAll(path, "\\", "/") // normalize
		pathInZip = strings.TrimPrefix(pathInZip, sourcePath)
		pathInZip = strings.TrimLeft(pathInZip, "/")
		f, err := w.Create(pathInZip)
		if err != nil {
			return fmt.Errorf("failed to zip directory (failed to create path in zip) %w", err)
		}

		_, err = io.Copy(f, file)
		if err != nil {
			return fmt.Errorf("failed to zip directory (failed to copy to zip) %w", err)
		}

		return nil
	}
	err = filepath.Walk(sourcePath, walker)
	if err != nil {
		return fmt.Errorf("failed to zip directory %w", err)
	}

	return nil
}

func Upload(ctx context.Context, resources *hazelcast.Map, prefix string, path string) error {

	chunkSize := 1 << 17
	chunkIndex := 0
	buffer := make([]byte, chunkSize)
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	n := 1
	for n > 0 {
		readCount := 0
		for {
			n, err = f.Read(buffer[readCount:])
			if err != nil {
				if err == io.EOF {
					n = 0
				} else {
					return fmt.Errorf("failed to read zip: %w", err)
				}
			}
			readCount += n
			if readCount == chunkSize || n == 0 {
				break
			}
		}
		if readCount > 0 {
			if readCount != chunkSize {
				buffer = buffer[:readCount]
				//buffer2 := make([]byte, readCount)
				//copy(buffer2, buffer[:readCount])
				//buffer = buffer2
			}
			//fmt.Println("  chunk of size ", len(buffer), " bytes ->  ", prefix+"_"+strconv.Itoa(chunkIndex))
			err = resources.Set(ctx, prefix+"_"+strconv.Itoa(chunkIndex), buffer)
			if err != nil {
				return fmt.Errorf("failed to upload chunk: %w", err)
			}
			chunkIndex += 1
		}
	}

	buffer = make([]byte, proto.IntSizeInBytes)
	binary.BigEndian.PutUint32(buffer, uint32(chunkIndex))
	err = resources.Set(ctx, prefix, buffer)
	if err != nil {
		return fmt.Errorf("failed to upload chunks count: %w", err)
	}

	return nil
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
