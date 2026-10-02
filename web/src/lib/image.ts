/**
 * Scales an image file to at most `max` px on its longest side and re-encodes it, so uploads
 * stay small. JPEG gets a white background (transparent PNGs would turn black); PNG keeps
 * transparency.
 */
export async function shrinkImage(file: File, max: number, type: "image/jpeg" | "image/png" = "image/jpeg"): Promise<string> {
  const url = URL.createObjectURL(file);
  try {
    const img = new Image();
    img.src = url;
    await img.decode();
    const scale = Math.min(1, max / Math.max(img.naturalWidth, img.naturalHeight));
    const canvas = document.createElement("canvas");
    canvas.width = Math.round(img.naturalWidth * scale);
    canvas.height = Math.round(img.naturalHeight * scale);
    const ctx = canvas.getContext("2d")!;
    if (type === "image/jpeg") {
      ctx.fillStyle = "#fff";
      ctx.fillRect(0, 0, canvas.width, canvas.height);
    }
    ctx.drawImage(img, 0, 0, canvas.width, canvas.height);
    return canvas.toDataURL(type, 0.85);
  } finally {
    URL.revokeObjectURL(url);
  }
}
