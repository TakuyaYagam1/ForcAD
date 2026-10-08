# Static Semgrep fixture only. Never import or execute this file.
import ast
import json
import pickle
import subprocess

import httpx
import requests
import yaml
from subprocess import run
from yaml import CSafeLoader, SafeLoader


def dynamic_code(source):
    # ruleid: forcad-python-dynamic-execution
    eval(source)
    # ruleid: forcad-python-dynamic-execution
    exec(source)
    # ok: forcad-python-dynamic-execution
    ast.literal_eval(source)
    # ok: forcad-python-dynamic-execution
    redis.eval(source)
    # ok: forcad-python-dynamic-execution
    json.loads(source)


def yaml_data(source):
    # ruleid: forcad-python-unsafe-yaml-loader
    yaml.load(source)
    # ruleid: forcad-python-unsafe-yaml-loader
    yaml.load_all(source, Loader=yaml.UnsafeLoader)
    # ok: forcad-python-unsafe-yaml-loader
    yaml.safe_load(source)
    # ok: forcad-python-unsafe-yaml-loader
    yaml.load(source, Loader=yaml.SafeLoader)
    # ok: forcad-python-unsafe-yaml-loader
    yaml.load_all(source, Loader=SafeLoader)
    # ok: forcad-python-unsafe-yaml-loader
    yaml.load(source, yaml.CSafeLoader)
    # ok: forcad-python-unsafe-yaml-loader
    yaml.load_all(source, Loader=CSafeLoader)


def processes(command):
    # ruleid: forcad-python-subprocess-shell
    subprocess.run(command, shell=True, check=True)
    # ruleid: forcad-python-subprocess-shell
    run(command, shell=True)
    # ok: forcad-python-subprocess-shell
    subprocess.run(["tool", command], check=True)
    # ok: forcad-python-subprocess-shell
    subprocess.run(["tool", command], shell=False)


def http_clients(url):
    # ruleid: forcad-python-disabled-tls
    requests.get(url, verify=False, timeout=5)
    # ruleid: forcad-python-disabled-tls
    httpx.AsyncClient(verify=False)
    # ok: forcad-python-disabled-tls
    requests.get(url, verify=True)
    # ok: forcad-python-disabled-tls
    httpx.get(url, verify="certificates.pem")


def serialized_data(source, file):
    # ruleid: forcad-python-pickle-load
    pickle.loads(source)
    # ruleid: forcad-python-pickle-load
    pickle.load(file)
    # ok: forcad-python-pickle-load
    json.loads(source)
