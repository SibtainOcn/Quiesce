# Quiesce

Un limpiador del sistema y optimizador de RAM rápido y ligero para Windows. Un único ejecutable, cero instalación, sin frameworks GUI pesados: solo una interfaz de terminal limpia con 12 pasos de limpieza personalizables para liberar espacio en disco y acelerar tu PC de forma segura.

---

## Qué limpia

| # | Paso de limpieza | Descripción | Por defecto |
|---|---|---|---|
| **1** | Temp de Windows | Archivos temporales del sistema creados por Windows | **ON** |
| **2** | Temp del usuario | Archivos temporales creados por las aplicaciones que ejecutas | **ON** |
| **3** | Prefetch | Caché de lanzamiento de aplicaciones de Windows | **ON** |
| **4** | Informes de errores de Windows | Volcados de memoria y archivos de registro de errores | **ON** |
| **5** | Caché de optimización de entrega | Archivos de distribución de actualizaciones de Windows descargados | **ON** |
| **6** | Caché de Windows Update | Archivos sobrantes de la instalación de actualizaciones | **ON** |
| **7** | Archivos de registro de Windows | Registros de actividad del sistema y las aplicaciones | **ON** |
| **8** | Caché del instalador | Archivos temporales del parche de Windows Installer | **ON** |
| **9** | Caché del resolver de DNS | Vacía los registros DNS en caché para refrescar la conexión | **ON** |
| **10** | Optimización de RAM | Limpia la memoria en espera y las listas de páginas sucias | **ON** |
| | ├ Vaciar lista modificada | Escribe las páginas sucias en disco para que puedan liberarse | **ON** |
| | ├ Vaciar lista de reserva | Libera la lista de páginas en caché/en espera | **ON** |
| | ├ Caché de archivos del sistema | Elimina los datos de archivos en caché del kernel | **OFF** (Opt-in) |
| | └ Recortar conjuntos de trabajo | Saca de memoria las apps activas — libera lo máximo, pero las apps las releen después desde el disco | **OFF** (Opt-in) |
| **11** | Papelera de reciclaje | Vacía la Papelera de reciclaje de Windows | **OFF** (Opt-in) |
| **12** | Limpieza profunda | Liberador de espacio de Windows completo (cleanmgr, todos los manejadores) | **OFF** (Se reinicia tras cada uso) |

Cada paso se puede personalizar en el menú de ajustes, y tus opciones se guardan automáticamente.

---

## Características principales

- **Optimización de RAM real**: a diferencia de los limpiadores de memoria falsos que solo fuerzan las apps al archivo de paginación, Quiesce pide directamente al kernel de Windows que vacíe la memoria modificada y purgue la lista de RAM en espera. Informa la memoria real liberada.
- **Gestión inteligente de servicios**: los servicios de Windows en segundo plano (como Windows Update y Delivery Optimization) se detienen de forma segura antes de limpiar sus archivos en caché y se reinician automáticamente después.
- **Seguro por defecto**: las opciones arriesgadas, como vaciar la Papelera de reciclaje, están desactivadas por defecto. La limpieza profunda del sistema se reinicia automáticamente a OFF tras cada ejecución para que no pueda repetirse por accidente.
- **Un solo archivo y portable**: no requiere instalador. Solo ejecuta `qc.exe` en cualquier lugar.

---

## Idioma / Internacionalización

Quiesce está internacionalizado (inglés y español) con la librería
[`itskreisler/i18n-go`](../kreisler-i18n):

- **Detección automática**: la interfaz sigue el idioma de pantalla de Windows.
- **Override**: añade una línea `LANGUAGE=` a `cleaner_config.dat` (junto a
  `qc.exe`) para forzar un idioma, p. ej. `LANGUAGE=en` o `LANGUAGE=es`.

Para añadir un nuevo idioma, crea `locales/active.<code>.toml` (copia
`active.en.toml` y tradúcelo), luego ejecuta `go generate ./...` para regenerar
las claves tipadas, y `go run github.com/itskreisler/i18n-go/cmd/i18n validate -dir locales`
para verificar que cada clave está traducida.

---

## Cómo usar

Ejecuta **`qc.exe`** como Administrador:

```text
  ENTER    Ejecuta el limpiador con la configuración actual
  F        Abre el menú de configuración

Controles de configuración:
  W / S    Mueve la selección Arriba / Abajo
  D / A    Activa / Desactiva el paso (ON / OFF)
  E        Guarda y vuelve al menú principal
```

---

## Verificar tu copia

Quiesce se ejecuta con derechos de Administrador, así que vale la pena
confirmar que el binario que tienes es el oficial. Cada build lleva su propia
identidad:

```bash
qc --version
```

Esto imprime la versión, el autor, el repositorio, la licencia y el SHA-256 del
propio ejecutable — y **no** requiere Administrador. Compara ese hash con la
suma de verificación publicada con la
[versión oficial](https://github.com/SibtainOcn/Quiesce/releases).
Si no coincide, el binario no es un build oficial.

---

## Compilar desde el código fuente

Requiere Go 1.23+ en Windows:

```bash
go build -o qc.exe
```

El hash del commit y la hora de compilación se incrustan automáticamente desde
git. Para fijar una cadena de versión explícita:

```bash
go build -ldflags "-X main.Version=2.2.0" -o qc.exe
```

---

## Requisitos

- **SO**: Windows 10 u 11 (64 bits)
- **Privilegios**: Administrador (solicitará UAC cuando ejecutes una limpieza)

### Descarga

**[⬇ Descargar qc.exe](https://github.com/SibtainOcn/Quiesce/releases/latest/download/qc.exe)** — siempre la última versión.

Colócalo en cualquier lugar de tu `PATH` (p. ej. `C:\Users\<tu-usuario>\bin`) para ejecutar `qc` desde cualquier terminal.

---

## Soporte y licencia

- **Licencia**: [GNU General Public License v3.0 o posterior](LICENSE) (`GPL-3.0-or-later`)

  Quiesce es software libre. Puedes usarlo, estudiarlo, compartirlo y
  modificarlo. Si distribuyes una versión modificada, esta también debe ser
  software libre bajo la GPL, con el código fuente disponible — para que cada
  usuario de un fork conserve las mismas libertades que tienes aquí. Esto
  importa para una herramienta que se ejecuta como Administrador: nadie debería
  tener que confiar en una versión que no puede leer.

  Quiesce se distribuye SIN NINGUNA GARANTÍA. Consulta [LICENSE](LICENSE) para
  los términos completos.
- **Soporte**: si Quiesce salvó tu PC de la basura, optimizó y mejoró su
  rendimiento, no dudes en apoyar el proyecto:

<a href="https://buymeacoffee.com/sibtainocn"><img src="https://cdn.buymeacoffee.com/buttons/v2/default-yellow.png" alt="Buy Me A Coffee" height="50"></a>
