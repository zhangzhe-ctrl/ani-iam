FROM docker.io/library/python@sha256:7415fbc3c9e4979cc717d92377ab2bc7b2b4a2af1ac03cc52b5f3f88efedaf3a

COPY probe.py /opt/cf01/probe.py
USER 65532:65532
ENTRYPOINT ["python3", "/opt/cf01/probe.py"]
