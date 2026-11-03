# Fuentes del MEF: endpoints y comportamiento observado

Ninguna de estas fuentes tiene API oficial documentada. Lo siguiente se verificó en vivo el **2026-09-10**. Si algo cambia, captura un fixture nuevo y actualiza esta página.

## Consulta Amigable: ejecución del gasto (`amigable`)

**Endpoint:** `GET https://apps5.mineco.gob.pe/transparencia/Navegador/Navegar_7.aspx`

Parece una app ASP.NET WebForms con estado (ViewState, postbacks), pero su URL de exportación es **stateless**:

```
?_uhc=yes&0=&1=E&2=01&3=&y=2026&ap=ActProy&cpage=1&psize=1000
```

- **Nombre del parámetro:** código de dimensión. **Valor:** la clave seleccionada. **Valor vacío:** la dimensión que se agrupa. **Orden:** la ruta de drill-down. `0=` es la raíz (TOTAL).
- `y` es el año (1999–hoy). `ap` es `ActProy` | `Actividad` | `Proyecto`. `cpage` y `psize` paginan, hasta 1000 filas.
- Funciona sin cookies ni sesión: verificado desde sesión nueva, ~1.1 s.

**Mapa de dimensiones** (barrido de los códigos 0–45; el encabezado oficial está en `td#ctl00_CPH1_Mt0_td1`):

| Código | Dimensión | Código | Dimensión |
|---|---|---|---|
| 1 | Nivel de Gobierno (E/R/M) | 22 | Trimestre |
| 2 | Sector | 23 | Mes |
| 3 | Pliego | 24 | Genérica |
| 4 | Unidad Ejecutora | 25 | Sub-Genérica |
| 5 | Departamento (ejecutora) | 26 | Detalle Sub-Genérica |
| 6 | Provincia | 27 | Específica |
| 7 | Municipalidad | 28 | Detalle Específica |
| 8 | Función | 30 | Categoría Presupuestal |
| 13 | Meta (~30 s) | 31 | Producto/Proyecto |
| 14 | Fuente de Financiamiento | 32 | Actividad/Acción/Obra (~8 s) |
| 15 | Rubro | 33 | División Funcional |
| 17 | Tipo de Recurso | 34 | Grupo Funcional (~5 s) |
| 21 | Departamento (meta) | 35 | Finalidad (~13 s) |
| 36 | Mancomunidad | 37 / 38 | Gob. Locales / Regionales – Mancomunidades |

- 9–12, 16 y 29 no devuelven filas.
- 18–20 (Genérica de Gasto, Modalidad, Elemento) son del clasificador antiguo y están vacíos en 2026.
- 39–45 solo devuelven TOTAL.

**Rarezas:**

- **Montos exactos en el radio:** `<input name="grp1" value="COD/PIA/PIM/COMPROMISO_ANUAL/COMPROMISO_MENSUAL/DEVENGADO/GIRADO/CERTIFICACION">`. Las celdas visibles están redondeadas.
- **Filas alternas** con `<tr class="alt" id="trN">`: el `id` no es el primer atributo.
- **Codificación mixta:** declara `charset=utf-8`, pero mezcla bytes Windows-1252 de la base de datos (`Funci\xf3n`) con UTF-8 real del marcado. Se decodifica byte a byte (`textenc.DecodeMixed`).
- **Dimensiones de tiempo sin presupuesto:** mes y trimestre traen PIA y PIM **vacíos** (`1///127861…`), porque el presupuesto es anual. Se reportan como `null`, no como 0.
- **Nombres de mes con apóstrofo** inicial (`'Enero`), un artefacto de Excel.
- **Fecha de actualización** en `span#ctl00_CPH1_LblLastUpdate` ("09 de septiembre de 2026."). Se actualiza diariamente.
- Con `_tgt=xls` la misma URL devuelve un "Excel" que en realidad es HTML en Windows-1252. No se usa.

**Nombres de entidades (base de `mef_entidades`):** las listas de dimensiones son también el directorio de entidades, y **el MEF abrevia sin criterio dentro de la misma página**:

```
009 | M. DE ECONOMIA Y FINANZAS          036 | MINISTERIO DE TRANSPORTES Y COMUNICACIONES
011 | M. DE SALUD                        040 | MINISTERIO DE DESARROLLO E INCLUSION SOCIAL
010 | M. DE EDUCACION                    037 | MINISTERIO DE VIVIENDA, CONSTRUCCION Y SANEAMIENTO
```

Por eso la búsqueda por nombre no puede exigir la palabra completa: `ministerio` se busca también como `m.`, y los conectores (`de`, `del`, `la`) se ignoran porque el MEF los incluye u omite a voluntad (`GOBIERNO REGIONAL DEL DEPARTAMENTO DE PUNO`). Las municipalidades sí usan el nombre completo (`MUNICIPALIDAD PROVINCIAL DE AREQUIPA`), pero conviven con mancomunidades que contienen el mismo topónimo.

**Verificación:** la suma por nivel en 2026 da PIA 257,561,619,143, PIM 287,487,003,991 y avance 62.2 %, idéntica a la fila TOTAL de la web (corte 2026-09-09). La ficha del pliego 011 (`M. DE SALUD`) devolvió 8,024.7 millones de devengado el 2026-10-03.

## Datos Abiertos (`datosabiertos`)

**Catálogo** (SPA Angular; rutas tomadas de su bundle JS):

- `POST https://datosabiertos.mef.gob.pe/Rest/PortalWebDatasets/v1.0/getDatasets` con `{"search","sort":null,"tags":[],"formats":[],"categories":[slug],"page"}`. Devuelve 10 por página, `countTotal`, `pagesCount`. **Filtra el SQL generado** en el campo `debug`.
- `POST …/Rest/PortalWebDatasetDetalle/v1.0/getDatasetDetalle` con `{"dataset": slug}`. Devuelve categoría, metadatos, `resources[]` (`resource_id`, `resource_url`, `resource_download_url_zip`).
- 74 datasets, 1024 recursos, 6 categorías: `presupuesto-publico` (34), `contabilidad-publica` (11), `inversion-publica` (9), `recursos-humanos-del-sector-publico` (8), `endeudamiento-y-tesoro-publico` (7), `abastecimiento-del-sector-publico` (5).
- UTF-8 correcto.
- El antiguo `datosabiertos.gob.pe` (CKAN) y `/api/3/action/*` ya no existen; `/api/3/action/status_show` devuelve la SPA.

**Datastore:** `GET https://api.datosabiertos.mef.gob.pe/DatosAbiertos/v1/datastore_search?resource_id&limit&offset&filters={json}&q`

- Backend PostgreSQL detrás de un wrapper Perl (`../WebService/DatosAbiertos/v1/*.pm`).
- **Éxito:** `{"help","sucess":"true","next","result":{…},"records":[…]}`. Ojo: `sucess` va con una sola "c". Los valores son texto y las **claves salen en orden de hash**, que cambia entre respuestas.
- **Error:** `{"oCod":"409","oDes":[{"err":"42703","sql":"SELECT \"COL1\",… FROM …"}],"iURI",…}`, a veces con HTTP 409. El SQL filtrado trae **todas las columnas válidas** y se aprovecha para el hint. `42P01` significa tabla inexistente.
- `records: null` sin filtros significa que el recurso no está cargado en el datastore. Pasa con archivos de varios GB, como `comparativo_gastos_2022_2026`.
- `filters` es igualdad exacta y distingue mayúsculas. No hay `include_total`.
- **Latencia:** sin filtro, 2–20 s por página de 5000 filas. **Con filtro o `q`, ~86 s por página**, porque recorre la tabla entera.
- `datastore_search_sql` responde 404.
- `dump?id=…&filters={}` es **asíncrono**: responde texto plano "Creando Consulta" con `Refresh: 2`. No se usa en v1.
- **Archivos:** `https://fs.datosabiertos.mef.gob.pe/datastorefiles/<nombre>.csv|.zip`.

## Invierte.pe: Seguimiento de Inversiones (`invierte`)

**Base:** `https://ofi5.mef.gob.pe/invierteWS`. Todos son `POST` form-urlencoded (endpoints de `/ssi/Scripts/repssi/*.js`).

| Endpoint | Parámetros | Devuelve |
|---|---|---|
| `/Ssi/traeDetInvSSI` | `id` (CUI), `tipo=SIAF` \| `SNIP` | Ficha completa (1 registro) |
| `/Dashboard/busInvNombreSSI` | `des_inv` (texto), `tipo=NOM` | ≤ 20 coincidencias, sin montos |
| `/Dashboard/traeDevengSSI` | `id`, `tipo=UEP` \| `ANIO` \| `MES` \| `FTE` \| `ESPECIF` | Ejecución por ejecutora / año / mes / fuente / específica |
| `/Ssi/traeContratoSeaceDWH` | `id`, `codsnip`, `vers=v2` | Contratos SEACE |
| `/Ssi/traeDatosObraCGR` | `id` | Obras InfoObras (Contraloría) |

**Rarezas:**

- **CUI inexistente:** devuelve `[]`, no 404.
- **Parámetro faltante:** HTTP 500 con página HTML de ASP.NET MVC ("The parameters dictionary contains a null entry for parameter 'id'…").
- **Fechas** en formato `/Date(1351101517000)/` (ms Unix). Se convierten a ISO en hora de Perú.
- **Identificadores numéricos** (`CODIGO_UNICO`, `SEC_EJEC`): se normalizan a texto.
- **Relleno:** cada registro trae decenas de campos en `null`, `0` o `""`. `compact()` los elimina.
- **Búsqueda por subcadena contigua:** `HOSPITAL AYACUCHO` no encuentra `HOSPITAL DE AYACUCHO`.
- **Latencia:** ~0.1–0.3 s.

**Verificación:**

- CUI 2194487: Municipalidad Provincial de Lima, DESACTIVADO PERMANENTE, viable 2014-02-25.
- CUI 2222116: ACTIVO, con 12 años de historia, 27 meses, 2 contratos SEACE y 1 obra.
