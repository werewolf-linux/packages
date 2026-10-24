"""Common graph and vector-store operations without optional tracing providers."""
import asyncio
import importlib.metadata
import json
import os
import tempfile

with tempfile.TemporaryDirectory() as directory:
    os.environ.update(LANGFLOW_CONFIG_DIR=directory, LANGFLOW_DO_NOT_TRACK='true',
                      LANGFLOW_AUTO_LOGIN='true', LFX_DO_NOT_TRACK='true')
    from langflow.main import setup_app
    from lfx.components.input_output.chat import ChatInput
    from lfx.components.input_output.chat_output import ChatOutput
    from lfx.graph import Graph
    import chromadb
    from chromadb.config import Settings

    names = {d.metadata['Name'].lower().replace('_', '-') for d in importlib.metadata.distributions()}
    assert not names.intersection({'arize-phoenix-otel', 'langfuse', 'openlayer', 'opik',
                                   'traceloop-sdk', 'uvloop', 'pyright', 'nodeenv', 'pytest',
                                   'ibm-db', 'oracledb', 'cassandra-driver', 'duckdb'})
    assert not any(n.startswith('opentelemetry-instrumentation') for n in names)
    app = setup_app()
    assert '/api/v1/flows/' in app.openapi()['paths']
    from langflow.services.utils import initialize_services, teardown_services
    incoming = ChatInput(should_store_message=False)
    outgoing = ChatOutput(should_store_message=False)
    outgoing.set(input_value=incoming.message_response)
    graph = Graph(start=incoming, end=outgoing)
    async def run_graph():
        await initialize_services()
        try:
            return await graph.arun(inputs=[{'input_value': 'consumer graph works'}])
        finally:
            await teardown_services()
    results = asyncio.run(run_graph())
    encoded = json.dumps([r.model_dump(mode='json') for r in results])
    assert 'consumer graph works' in encoded, encoded
    store = chromadb.PersistentClient(path=directory + '/vectors', settings=Settings(anonymized_telemetry=False))
    collection = store.create_collection('consumer_documents', embedding_function=None)
    collection.add(ids=['one', 'two'], embeddings=[[1., 0., 0.], [0., 1., 0.]],
                   documents=['first document', 'second document'])
    result = collection.query(query_embeddings=[[1., 0., 0.]], n_results=1)
    assert result['ids'] == [['one']], result
print('PASS: API schema, chat graph execution and local vector retrieval')
