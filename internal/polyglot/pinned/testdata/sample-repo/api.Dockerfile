FROM node:22-alpine
WORKDIR /api
COPY . .
CMD ["node", "server.js"]
