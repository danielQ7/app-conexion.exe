# Conexión Remota (uso personal)

App tipo AnyDesk para **red local**: controlá hasta **2 PCs** desde una, viendo la
pantalla y manejando el mouse y el teclado. Es un **solo `.exe`** para Windows, sin
instalar nada en las PCs (no usa internet ni servidores externos).

> Pensada para controlar tus propias PCs en tu propia red (LAN/WiFi de casa). No
> es para acceder a equipos ajenos.

## Cómo se usa

1. Copiá `conexion-remota.exe` a cada PC.
2. **En la PC que querés controlar** (la que se comparte): doble clic al `.exe` y
   elegí la opción **1) Permitir que controlen ESTA PC**. Te muestra su **IP** y una
   **clave**. Dejala abierta.
3. **En la PC desde la que vas a controlar**: doble clic al `.exe` y elegí
   **2) Controlar otras PCs**. Se abre el navegador con el **panel**. Escribí la IP
   y, al conectar, la clave de la otra PC. Podés poner 2 PCs al mismo tiempo.

La opción **3) Las dos cosas** sirve si querés que una misma PC comparta y controle.

### Primera vez: permiso de red (Firewall de Windows)
La primera vez que compartís una PC, Windows pregunta por el Firewall. Marcá
**Redes privadas** y aceptá, así la otra PC la puede ver.

## Opciones (línea de comandos, opcional)

```
conexion-remota.exe -modo servidor            # compartir esta PC (puerto 7070)
conexion-remota.exe -modo panel               # abrir el panel de control
conexion-remota.exe -modo ambos               # compartir y controlar
  -puerto 7070        puerto del servidor
  -clave MiClave      fija la clave (si no, usa clave.txt o una aleatoria)
```

Para una clave fija y fácil: creá un archivo `clave.txt` junto al `.exe` con la
clave adentro.

## Seguridad

- El acceso pide **clave**; tras 5 intentos fallidos se bloquea esa IP 1 minuto.
- El **panel** de control solo escucha en `127.0.0.1` (no se expone a la red).
- Es tráfico **sin cifrar** dentro de la LAN: usala en una red de confianza (tu
  casa), no en WiFi públicas.

## Compilar desde el código (solo si querés rehacer el `.exe`)

Necesitás [Go](https://go.dev/dl/) instalado. Después:

```
build.bat            # en Windows, genera conexion-remota.exe
```

o manualmente, desde cualquier sistema:

```
go build -ldflags "-s -w" -o conexion-remota.exe .
```

La captura de pantalla y el control de mouse/teclado usan las APIs de Windows
(GDI / user32), así que el `.exe` final **solo funciona en Windows**. El código
compila también en Linux/Mac pero ahí usa una pantalla de prueba (para desarrollo).
