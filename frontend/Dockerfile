FROM node:26-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM deps AS dev
COPY . .
EXPOSE 5173
CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0", "--port", "5173"]

FROM deps AS runtime
ARG VITE_API_URL=http://localhost:8080
ENV VITE_API_URL=$VITE_API_URL
COPY . .
RUN npm run build

EXPOSE 5173
CMD ["sh", "-c", "npm run preview -- --host 0.0.0.0 --port ${PORT:-5173}"]
