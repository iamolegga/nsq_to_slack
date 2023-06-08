FROM scratch
COPY nsq_to_slack /
ENTRYPOINT ["/nsq_to_slack"]
