# SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
FROM node:20-alpine AS build
WORKDIR /app

# Install deps first to leverage Docker layer cache
COPY package*.json ./
RUN npm ci

# Copy sources and build
COPY . .
RUN npm run build

FROM nginx:alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
