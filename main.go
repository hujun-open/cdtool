package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/hujun-open/myflags/v2"
	"github.com/spf13/cobra"
	v1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CLI struct {
	KubeCfgPath string `alias:"kubeconf" usage:"path to k8s config"`
	List        struct {
		All bool `usage:"list all jobs if true"`
	} `action:"ListJobs" usage:"list existing jobs"`
	Show struct {
		Name string `noun:"1" usage:"job name"`
		NS   string `usage:"k8s namespace where the job is"`
	} `action:"ShowJob" usage:"list existing jobs"`
	Upload struct {
		NS            string `usage:"k8s namespace where the job is"`
		DownloadImage string `usage:"download container image"`
		BuildImage    string `usage:"build container image"`
		Remote        struct {
			Src  string `noun:"1" usage:"disk image source url"`
			Tag  string `noun:"2" usage:"container disk image tag"`
			Wait bool   `usage:"wait for completion"`
		} `action:"UploadImgRemote" usage:"upload source disk image on remote server to the registry with specified tag"`
		Local struct {
			File           string     `noun:"1" usage:"local disk image file path"`
			Tag            string     `noun:"2" usage:"container disk image tag"`
			HttpPort       int        `alias:"listenport" usage:"local http server listening port"`
			HttpListenAddr netip.Addr `required:"" alias:"listenaddr" usage:"local http listening address"`
		} `action:"UploadImgLocal" usage:"upload local disk image file to the registry with specified tag"`
	} `action:""`
}

const (
	defaultHTTPPort = 8899
	appLabelKey     = "app.kubernetes.io/name"
	appLabelValue   = "cdtool"
)

func defCLI() *CLI {
	r := new(CLI)
	kpath := ""
	if home := homedir.HomeDir(); home != "" {
		kpath = filepath.Join(home, ".kube", "config")
	}
	r.KubeCfgPath = kpath
	r.Upload.NS = "default"
	r.Upload.DownloadImage = "busybox:stable"
	r.Upload.BuildImage = "ghcr.io/hujun-open/cdtool:latest"
	r.Upload.Local.HttpPort = defaultHTTPPort
	return r
}

func (cli *CLI) getClnt() (client.Client, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", cli.KubeCfgPath)
	if err != nil {
		panic(fmt.Sprintf("Error building kubeconfig from %s: %v", cli.KubeCfgPath, err))
	}
	scheme := runtime.NewScheme()
	v1.AddToScheme(scheme)
	batchv1.AddToScheme(scheme)
	return client.New(cfg, client.Options{Scheme: scheme})
}

func getJobName() string {
	return fmt.Sprintf("cdtool-%v-%v", time.Now().Format("060102-150405"), rand.String(4))
}

func (cli *CLI) UploadImgLocal(cmd *cobra.Command, args []string) {
	if cli.Upload.Local.File == "" {
		log.Fatal("local image file not specified")
	}
	if cli.Upload.Local.Tag == "" {
		log.Fatal("tag is not specified")
	}
	clnt, err := cli.getClnt()
	if err != nil {
		log.Fatal(err)
	}
	jobName := getJobName()
	tmpDir, err := os.MkdirTemp("", fmt.Sprintf("%v-*", jobName))
	if err != nil {
		log.Fatal(err)
	}
	targetFile := filepath.Join(tmpDir, filepath.Base(cli.Upload.Local.File))
	command := exec.Command("cp", cli.Upload.Local.File, targetFile)
	err = command.Run()
	if err != nil {
		log.Fatal(err)
	}
	//start http server
	listenAddr := netip.AddrPortFrom(cli.Upload.Local.HttpListenAddr, uint16(cli.Upload.Local.HttpPort))
	go func() {
		err = http.ListenAndServe(listenAddr.String(), http.FileServer(http.Dir(tmpDir)))
		if err != nil {
			log.Fatal(err)
		}
	}()
	job := cli.newJob(jobName, fmt.Sprintf("http://%v/%v", listenAddr, filepath.Base(cli.Upload.Local.File)), cli.Upload.Local.Tag)
	err = clnt.Create(cmd.Context(), job)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("job %v created in namespace %v", job.Name, job.Namespace)
	cli.waitForJob(cmd.Context(), clnt, job.Namespace, job.Name)
}

func (cli *CLI) newJob(name, url, tag string) *batchv1.Job {
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cli.Upload.NS,
			Labels: map[string]string{
				appLabelKey: appLabelValue,
			},
		},
	}
	//init container
	downloadContainer := corev1.Container{
		Name:    "download",
		Image:   cli.Upload.DownloadImage,
		Command: []string{"sh", "-c", "wget $URL -O /save/disk.img"},
		Env: []corev1.EnvVar{
			{
				Name:  "URL",
				Value: url,
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "saveplace",
				MountPath: "/save",
			},
		},
	}
	job.Spec.Template.Spec.InitContainers = append(job.Spec.Template.Spec.InitContainers, downloadContainer)
	//build container
	buildContainer := corev1.Container{
		Name:    "buildandpush",
		Image:   cli.Upload.BuildImage,
		Command: []string{"sh", "-c", "/buildandpush.sh"},
		Env: []corev1.EnvVar{
			{
				Name:  "TAG",
				Value: tag,
			},
			{
				Name:  "INSECURE",
				Value: "true",
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "saveplace",
				MountPath: "/save",
			},
			{
				Name:      "varlibcontainers",
				MountPath: "/var/lib/containers",
			},
		},
		SecurityContext: &corev1.SecurityContext{
			Privileged: ReturnPointerVal(true),
		},
	}
	job.Spec.Template.Spec.Containers = append(job.Spec.Template.Spec.Containers, buildContainer)
	job.Spec.Template.Spec.Volumes = append(job.Spec.Template.Spec.Volumes,
		corev1.Volume{
			Name: "varlibcontainers",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: new(corev1.EmptyDirVolumeSource),
			},
		},
		corev1.Volume{
			Name: "saveplace",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: new(corev1.EmptyDirVolumeSource),
			},
		},
	)
	job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
	return &job
}
func (cli *CLI) waitForJob(ctx context.Context, clnt client.Client, ns, name string) {
	jobKey := types.NamespacedName{Namespace: ns, Name: name}
	getJob := new(batchv1.Job)
	fmt.Println("waiting for completion...")
	startTime := time.Now()
	for {
		err := clnt.Get(ctx, jobKey, getJob)
		if err != nil {
			log.Fatal(err)
		}
		if getJob.Status.Succeeded == 1 {
			fmt.Println("done")
			return
		}
		time.Sleep(time.Second)
		fmt.Printf("\rfailed %d times, time elapsed %v...", getJob.Status.Failed, time.Since(startTime).Round(time.Second))

	}
}

func (cli *CLI) ShowJob(cmd *cobra.Command, args []string) {
	jobs := new(batchv1.JobList)
	clnt, err := cli.getClnt()
	if err != nil {
		log.Fatal(err)
	}
	listOpts := []client.ListOption{
		client.InNamespace(cli.Show.NS),
		client.MatchingLabels{appLabelKey: appLabelValue},
	}
	if cli.Show.Name != "" {
		listOpts = append(listOpts, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector("metadata.name", cli.Show.Name),
		})
	}
	err = clnt.List(cmd.Context(), jobs, listOpts...)
	if err != nil {
		log.Fatal(err)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	defer w.Flush()
	fmt.Fprintln(w, "name\tsrc\ttag\tsucceed\tfinish time")
	for _, job := range jobs.Items {
		finishTImeStr := "n/a"
		if job.Status.CompletionTime != nil {
			finishTImeStr = job.Status.CompletionTime.Format(time.DateTime)
		}
		fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%v\n", job.Namespace+"/"+job.Name,
			job.Spec.Template.Spec.InitContainers[0].Env[0].Value,
			job.Spec.Template.Spec.Containers[0].Env[0].Value,
			job.Status.Succeeded == 1, finishTImeStr)
	}

}
func (cli *CLI) ListJobs(cmd *cobra.Command, args []string) {
	jobs := new(batchv1.JobList)
	clnt, err := cli.getClnt()
	if err != nil {
		log.Fatal(err)
	}
	listOpts := []client.ListOption{
		client.MatchingLabels{appLabelKey: appLabelValue},
	}
	err = clnt.List(cmd.Context(), jobs, listOpts...)
	if err != nil {
		log.Fatal(err)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	defer w.Flush()
	fmt.Fprintln(w, "name\tfailed\tsuccess\tcompletion time")
	for _, job := range jobs.Items {
		if job.Status.Succeeded == 1 && !cli.List.All {
			continue
		}
		finishTImeStr := "n/a"
		if job.Status.CompletionTime != nil {
			finishTImeStr = job.Status.CompletionTime.Format(time.DateTime)
		}
		fmt.Fprintf(w, "%v\t%v\t%v\t%v\n", job.Namespace+"/"+job.Name,
			job.Status.Failed, job.Status.Succeeded == 1,
			finishTImeStr)
	}

}

func (cli *CLI) UploadImgRemote(cmd *cobra.Command, args []string) {
	if cli.Upload.Remote.Src == "" {
		log.Fatal("src is not specified")
	}
	if cli.Upload.Remote.Tag == "" {
		log.Fatal("tag is not specified")
	}
	clnt, err := cli.getClnt()
	if err != nil {
		log.Fatal(err)
	}
	job := cli.newJob(getJobName(), cli.Upload.Remote.Src, cli.Upload.Remote.Tag)
	err = clnt.Create(cmd.Context(), job)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("job %v created in namespace %v", job.Name, job.Namespace)
	if cli.Upload.Remote.Wait {
		cli.waitForJob(cmd.Context(), clnt, job.Namespace, job.Name)
	}
}

func ReturnPointerVal[T any](val T) *T {
	r := new(T)
	*r = val
	return r
}

func main() {
	cli := defCLI()
	filler := myflags.NewFiller("cdtool", "kubevirt container disk tool", myflags.WithShellCompletionCMD())
	err := filler.Fill(cli)
	if err != nil {
		panic(err)
	}
	err = filler.Execute()
	if err != nil {
		panic(err)
	}
}
