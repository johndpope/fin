# base image
FROM node:10.15.0
 
RUN mkdir /usr/src/app
# install and cache app dependencies
RUN mkdir /usr/src/app/frontend
WORKDIR /usr/src/app/frontend
COPY ./frontend/package*.json ./
RUN npm install
RUN npm install -g @vue/cli @vue/cli-service vue-template-compiler vuetify

WORKDIR /usr/src/app
RUN apt-get update && apt-get install -y gcc make python3 python3-pip\
    && pip3 install --no-cache-dir sqlite-web
RUN sh -c "$(wget -O- https://github.com/deluan/zsh-in-docker/releases/download/v1.1.1/zsh-in-docker.sh)" -- \
    -t https://github.com/denysdovhan/spaceship-prompt \
    -a 'SPACESHIP_PROMPT_ADD_NEWLINE="false"' \
    -a 'SPACESHIP_PROMPT_SEPARATE_LINE="false"' \
    -p git \
    -p ssh-agent \
    -p https://github.com/zsh-users/zsh-autosuggestions \
    -p https://github.com/zsh-users/zsh-completions
ENV PATH /usr/local/lib/python3.7/site-packages:$PATH

COPY .env /usr/src/app/frontend
COPY ./entrypoints/start-dev.sh /
RUN chmod +x /start-dev.sh
CMD ["/bin/bash", "-c", "source /start-dev.sh"]