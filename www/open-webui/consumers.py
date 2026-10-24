"""Exercise common local chat, authentication and document-storage consumers."""
import importlib.metadata
import io
import os

# No remote models or telemetry are needed for these application tests.
os.environ.update(WEBUI_ADMIN_EMAIL='admin@example.test',
                  WEBUI_ADMIN_PASSWORD='consumer-test-password-123',
                  OFFLINE_MODE='true', ENABLE_VERSION_UPDATE_CHECK='false')
from fastapi.testclient import TestClient
from open_webui.main import app
from open_webui.storage.provider import LocalStorageProvider
from open_webui.retrieval.loaders.local import TextLoader
from langchain_text_splitters import RecursiveCharacterTextSplitter

names = {d.metadata['Name'].lower().replace('_', '-') for d in importlib.metadata.distributions()}
omitted = names.intersection({'brotli', 'brotlicffi', 'aiodns', 'pycares', 'ldap3',
                              'starlette-compress', 'zstandard', 'socksio'})
assert not omitted, omitted
assert not any(n.startswith(('opentelemetry-', 'azure-')) for n in names)
with TestClient(app) as client:
    response = client.get('/health')
    assert response.status_code == 200, response.text
    response = client.post('/api/v1/auths/signin', json={
        'email': 'admin@example.test', 'password': 'consumer-test-password-123'})
    assert response.status_code == 200, response.text
    token = response.json()['token']
    client.headers['Authorization'] = 'Bearer ' + token
    response = client.post('/api/v1/chats/new', json={'chat': {
        'title': 'Consumer test', 'messages': [{'role': 'user', 'content': 'Hello'}]}})
    assert response.status_code == 200, response.text
    chat_id = response.json()['id']
    response = client.get('/api/v1/chats/' + chat_id)
    assert response.status_code == 200, response.text
    assert response.json()['chat']['messages'][0]['content'] == 'Hello'
    assert client.delete('/api/v1/chats/' + chat_id).status_code == 200
    response = client.post('/api/v1/auths/signin', json={
        'email': 'admin@example.test', 'password': 'wrong'})
    assert response.status_code == 400, response.text

body = b'Consumer document for retrieval. ' * 20
contents, path = LocalStorageProvider.upload_file(io.BytesIO(body), 'consumer.txt', {})
assert contents == body
assert LocalStorageProvider.get_file(path) == path
documents = TextLoader(path).load()
chunks = RecursiveCharacterTextSplitter(chunk_size=120, chunk_overlap=20).split_documents(documents)
assert len(chunks) > 1 and all('Consumer' in chunk.page_content for chunk in chunks)
LocalStorageProvider.delete_file(path)
assert not os.path.exists(path)
print('PASS: login, chat persistence, local documents and text retrieval without optional libraries')
