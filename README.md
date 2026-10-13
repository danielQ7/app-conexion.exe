# Conexión Remota (uso personal)

App tipo AnyDesk para controlar hasta **2 PCs** desde una, viendo la pantalla y
manejando el mouse y el teclado. Es un **solo `.exe`** para Windows, sin instalar
nada aparte de la propia app.

Funciona en dos escenarios:

- **Misma red (WiFi/LAN de casa):** directo, usás la IP local (192.168.x).
- **Redes distintas (por internet):** con **Tailscale** (ver abajo), usás la IP de
  Tailscale (100.x). Es la forma recomendada para tus propias PCs: gratis, cifrada
  y sin tocar el router.

> Pensada para controlar **tus propias PCs**. No es para acceder a equipos ajenos.

## Controlar por INTERNET (PCs en redes distintas) — con Tailscale

Dos PCs en WiFis diferentes no se ven entre sí porque cada router las esconde
detrás de NAT. Tailscale crea una red privada segura (VPN WireGuard) **entre tus
propias PCs**, por internet, sin configurar el router.

1. Instalá **Tailscale** en las 2 (o 3) PCs: https://tailscale.com/download
2. Iniciá sesión en todas con **la misma cuenta** (Google, Microsoft, etc.).
3. En cada PC, Tailscale le da una IP fija tipo `100.x.y.z` (la ves en la app de
   Tailscale, o con `tailscale ip`). Esa IP funciona desde cualquier lugar.
4. Usá la app igual que abajo, pero en el panel poné la **IP de Tailscale** de la
   otra PC en vez de la IP local. (Cuando compartís una PC, la app te muestra su
   IP de Tailscale etiquetada como "INTERNET".)

Ventaja: solo tus equipos con tu cuenta pueden verse; el tráfico va cifrado por
Tailscale aunque estés en otra ciudad o en datos del celular.

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
- **Por internet usá Tailscale**: así el tráfico va cifrado y solo tus equipos se
  ven entre sí. La app en sí manda los datos sin cifrar, por eso **no** conviene
  abrir el puerto 7070 directo a internet; dejá que Tailscale (o tu LAN de
  confianza) sea el único camino.

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
