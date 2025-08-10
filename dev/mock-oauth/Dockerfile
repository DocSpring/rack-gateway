FROM node:18-alpine

WORKDIR /app

# Copy package files
COPY package.json ./
RUN npm install --only=production

# Copy application code
COPY server.js ./

EXPOSE 3001

USER node

CMD ["npm", "start"]