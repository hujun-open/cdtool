#!/bin/bash
# This scirpt build kubevirt container disk image from "/save/disk.img" and push it into registry using buildah;
# mandatory input env vars: 
#    TAG: the image tag
# following env vars are optional:
# if INSECURE env is set, then the registry is insecure 
# if AUTHFILE env is set, then use /authfile.json as the authfile to login to registry
# if CUSTOMCA env is set, then use CA certificates in /CA/ 

: "${TAG:? 'Error: TAG environment variable is not set.'}"
cd /save
cat << END > Dockerfile
FROM scratch
ADD --chown=107:107 disk.img /disk/
END
buildah --storage-driver vfs --isolation chroot build -f Dockerfile -t $TAG .
if [ -n "$INSECURE" ]; then
    echo "using insecure registry"
    buildah --storage-driver vfs --isolation chroot push --tls-verify=false $TAG
else
    authfile=""
    if [ -n "$AUTHFILE" ]; then
        echo "using authfile"
        authfile="--authfile /authfile.json"
    fi
    customca=""
    if [ -n "$CUSTOMCA" ]; then
        echo "using custom CA"
        customca="--cert-dir /CA/"
    fi
    buildah --storage-driver vfs --isolation chroot push $authfile $customca $TAG
fi
