FROM python:3.11-slim-trixie

ENV PYTHONUNBUFFERED=1
ENV PYTHONPATH=${PYTHONPATH}:/app
ENV HOME=/home/forcad

COPY backend/requirements.txt /requirements.txt
COPY ./checkers/requirements.txt /checker_requirements.txt
RUN apt-get update \
    && apt-get upgrade -y --no-install-recommends \
    && apt-get install -y --no-install-recommends gcc libc6-dev libpq-dev libpq5 \
    && python -m pip install --no-cache-dir --upgrade \
        pip==26.2.1 setuptools==84.0.0 wheel==0.48.0 jaraco.context==6.1.2 \
    && python -m pip install --no-cache-dir -r /requirements.txt \
    && python -m pip install --no-cache-dir -r /checker_requirements.txt \
    && apt-mark manual libpq5 \
    && apt-get purge -y --auto-remove gcc libc6-dev libpq-dev \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/* \
    && python -m pip uninstall -y pip \
    && rm -rf /usr/local/lib/python3.11/ensurepip

COPY docker_config/await_start.sh /await_start.sh
COPY docker_config/db_check.py /db_check.py
COPY docker_config/check_initialized.py /check_initialized.py

RUN chmod +x /await_start.sh \
    && groupadd --gid 10001 forcad \
    && useradd --uid 10001 --gid forcad --create-home --shell /usr/sbin/nologin forcad \
    && chown forcad:forcad /home/forcad \
    && chmod 0750 /home/forcad

###### SHARED PART END ######

########## CUSTOMIZE ##########

ENV PWNLIB_NOTERM=true

# uncomment blocks to enable features

### selenium (chromedriver) dependencies (from https://github.com/joyzoursky/docker-python-chromedriver) ###
################
#RUN wget -q -O - https://dl-ssl.google.com/linux/linux_signing_key.pub | apt-key add -
#RUN sh -c 'echo "deb [arch=amd64] http://dl.google.com/linux/chrome/deb/ stable main" >> /etc/apt/sources.list.d/google-chrome.list'
#RUN apt-get update \
#    && apt-get install -y --no-install-recommends google-chrome-stable unzip \
#    && apt-get clean \
#    && rm -rf /var/lib/apt/lists/*
#RUN wget -O /tmp/chromedriver.zip http://chromedriver.storage.googleapis.com/`curl -sS chromedriver.storage.googleapis.com/LATEST_RELEASE`/chromedriver_linux64.zip
#RUN unzip /tmp/chromedriver.zip chromedriver -d /usr/local/bin/
################

COPY ./checkers /checkers

########## END CUSTOMIZE ##########

COPY backend /app

COPY ./docker_config/celery/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

USER forcad:forcad

CMD ["/entrypoint.sh"]
