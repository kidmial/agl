// internal/armor/web_rescue_tls.go
// =====================================================================
// КОМПОНЕНТ: TLS-ШЛЮЗ КАНАЛА СПАСЕНИЯ (M3f, v8.1.3)
// РОЛЬ: Самоподписанный ECDSA P-256 сертификат при первом старте.
//
//	Есть ключи → НЕ трогаем (TOFU-пиннинг через SHA-256 отпечаток в логе).
//	Срок 10 лет — канал спасения не должен протухать в аварийный момент.
//
// =====================================================================
package armor

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"time"

	"agl/config"
	"agl/internal/sys"
)

const (
	rescueCertDir  = config.DirEtc + "/certs"
	rescueCertPath = rescueCertDir + "/server.crt"
	rescueKeyPath  = rescueCertDir + "/server.key"
)

// RescueTLSReady — индикатор для главного меню: оба файла ключей на месте.
// Два лёгких os.Stat, без exec — безопасно для тикера 1 Гц.
func RescueTLSReady() bool {
	if _, err := os.Stat(rescueCertPath); err != nil {
		return false
	}
	_, err := os.Stat(rescueKeyPath)
	return err == nil
}

// ensureRescueTLS гарантирует наличие самоподписанной пары ключей.
// Ошибка → вызывающий уходит в fail closed (сервер НЕ стартует).
func ensureRescueTLS() error {
	if RescueTLSReady() {
		return nil
	}
	if err := os.MkdirAll(rescueCertDir, 0700); err != nil {
		return fmt.Errorf("не удалось создать директорию сертификатов: %w", err)
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("генерация ECDSA-ключа: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("генерация серийника: %w", err)
	}

	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "AmberGuard Rescue Gate", Organization: []string{"Amber Guard Lab"}},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0), // 10 лет
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("создание сертификата: %w", err)
	}

	keyOut, err := os.OpenFile(rescueKeyPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("файл ключа: %w", err)
	}
	if keyDER, errKey := x509.MarshalECPrivateKey(priv); errKey == nil {
		_ = pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	}
	_ = keyOut.Close()

	crtOut, err := os.OpenFile(rescueCertPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("файл сертификата: %w", err)
	}
	_ = pem.Encode(crtOut, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	_ = crtOut.Close()

	sum := sha256.Sum256(der)
	sys.LogAction("WEB_RESCUE", "TLS_CERT_GENERATED", "SUCCESS",
		fmt.Sprintf("Сгенерирован самоподписанный TLS-сертификат (ECDSA P-256, 10 лет). SHA-256 отпечаток для пиннинга: %s", hex.EncodeToString(sum[:])))
	return nil
}
