# Розгортання в Kubernetes

## Швидке розгортання

```bash
# Створення секрету з URL вашої бази даних
kubectl create secret generic postgres-mcp-secret \
  --from-literal=database-url="postgres://user:pass@your-db-host:5432/your-db" \
  --from-literal=openai-api-key="your-openai-key"

# Розгортання postgres-mcp
kubectl apply -f deployment.yaml

# Перевірка статусу
kubectl get pods -l app=postgres-mcp-server

# Тестування (проброс портів)
kubectl port-forward service/postgres-mcp-service 8080:8080
curl http://localhost:8080/healthz
```

## Конфігурація

- Оновіть `postgres-mcp.yourdomain.com` в Ingress на ваш реальний домен
- Змініть ліміти ресурсів відповідно до ваших потреб
- За потреби додайте конфігурацію SSL/TLS

## Нотатки для продакшену

- Використовуйте належне керування секретами (не літеральні значення)
- Налаштуйте моніторинг і логування
- Налаштуйте належний ingress із SSL-сертифікатами
- Розгляньте використання HorizontalPodAutoscaler для масштабування
