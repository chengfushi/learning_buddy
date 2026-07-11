# agent 多阶段构建
FROM python:3.11-slim AS build
WORKDIR /src
COPY requirements.txt .
RUN pip install --no-cache-dir --user -r requirements.txt
COPY . .
ENV PATH=/root/.local/bin:$PATH
EXPOSE 8000
CMD ["python", "main.py"]
