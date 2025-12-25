# overview
cdtool is a CLI tool + a container image to covert a qcow2 or raw disk image to [Kubevirt container disk image](https://kubevirt.io/user-guide/storage/disks_and_volumes/#containerdisk).

It is achieved by using the cli tool to create k8s job to fetch disk image file from various sources, convert it to kubevirt container disk image and push to the registry. 

note: currently cdtool only supports insecure registry that doesn't require any authentication

## installation
just download the cli tool from the release page

## usage
since actual job is done via k8s job, so access to a k8s cluster is require to run this tool.


### disk image from remote source
`cdtool upload remote <Src> <Tag> [flags]`; `Src` here could http, https, ftp URL supported by wget, while `TAG` is the name tag of target container disk image like `example.com/vmdisk:v1`

example:
`cdtool upload remote http://exampledownload.net/disk.qcow2 example.com/vmdisk:v1`

### local disk image file
`cdtool upload local <File> <Tag> --listenaddr <addr> [flags]` `File` is the path of local disk image file,`addr` is an local interface address that is reachable from k8s workers, while `TAG` is the name tag of target container disk image like `example.com/vmdisk:v1`