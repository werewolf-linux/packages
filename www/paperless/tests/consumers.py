"""Exercise the local OCR path after removing optional cloud/dashboard packages."""
import importlib.metadata
from pathlib import Path
import tempfile
from PIL import Image, ImageDraw, ImageFont
import ocrmypdf
from pdfminer.high_level import extract_text

names = {d.metadata['Name'].lower().replace('_', '-') for d in importlib.metadata.distributions()}
assert not names.intersection({'flower', 'azure-ai-documentintelligence', 'azure-core', 'sqlite-vec'})
with tempfile.TemporaryDirectory() as directory:
    root = Path(directory)
    page = Image.new('RGB', (1800, 600), 'white')
    ImageDraw.Draw(page).text((80, 180), 'INVOICE 12345', fill='black',
                             font=ImageFont.truetype('/usr/share/fonts/TTF/DejaVuSans.ttf', 100))
    source = root / 'scan.pdf'
    page.save(source, resolution=200)
    result = root / 'searchable.pdf'
    ocrmypdf.ocr(source, result, language=['eng'], output_type='pdf', optimize=0,
                jobs=1, progress_bar=False)
    text = extract_text(result)
    assert 'INVOICE' in text and '12345' in text, text
print('PASS: scanned PDF OCR creates searchable invoice text')
