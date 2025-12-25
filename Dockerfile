from debian:stable-slim
RUN apt-get update;apt-get install -y ca-certificates buildah
COPY buildandpush.sh /
RUN chmod +x /buildandpush.sh