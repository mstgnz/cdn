# Migration Guide

## Migrating from v1.x to v2.x

1. **Configuration Changes**
   - Add new environment variables using `.env.example` as reference
   - Update Redis cache configuration

2. **API Changes**
   - `/upload` endpoint now expects `multipart/form-data`
   - Resize parameters moved from query string to form data

3. **Database Changes**
   - Cache system migrated to Redis
   - MinIO bucket structure updated

4. **Required Steps**
   ```bash
   # 1. Stop the service
   systemctl stop cdn-service

   # 2. Install new version
   git pull
   go build

   # 3. Update configuration
   cp .env.example .env
   nano .env

   # 4. Clear cache
   redis-cli FLUSHALL

   # 5. Start service
   systemctl start cdn-service
   ``` 