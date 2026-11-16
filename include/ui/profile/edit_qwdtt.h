#pragma once
#include "profile_editor.h"
#include "include/configs/outbounds/qwdtt.h"
#include <QComboBox>
#include <QLineEdit>
#include <QSpinBox>
#include <QWidget>
#include <array>
#include <QSet>
class QFormLayout;
class QProcess;
class QLabel;
class QPushButton;
class QTemporaryDir;

class EditQWDTT : public QWidget, public ProfileEditor {
    Q_OBJECT
  public:
    explicit EditQWDTT(QWidget *parent = nullptr);
    ~EditQWDTT() override;
    void onStart(std::shared_ptr<Configs::Profile> ent) override;
    bool onEnd() override;

  private:
    std::shared_ptr<Configs::Profile> ent;
    QComboBox *mode;
    QLineEdit *password;
    QComboBox *hashSource;
    QSpinBox *autoHashCount;
    QFormLayout *form;
    std::array<QLineEdit *, Configs::qwdtt::MaxHashes> hashes;
    std::array<QWidget *, Configs::qwdtt::MaxHashes> hashRows;
    std::array<QLabel *, Configs::qwdtt::MaxHashes> hashStatus;
    QPushButton *checkHashes;
    QProcess *hashProcess = nullptr;
    std::unique_ptr<QTemporaryDir> hashCheckDirectory;
    QByteArray hashCheckOutput;
    QString hashCheckFailure;
    QList<int> checkingSlots;
    bool hashCheckStopped = false;
    QSet<int> completedHashChecks;
    int activeCheckingSlot = -1;
    void stopHashCheck();
    void consumeHashCheckOutput();
    QSpinBox *rawPort;
    QSpinBox *directPort;
    QLineEdit *diagKey;
    QLineEdit *customTurnIps;
    QComboBox *workers;
    void updateWorkers(int requested = -1);
    QComboBox *transport;
    QComboBox *obfs;
    QComboBox *authorization;
    QComboBox *captcha;
    QComboBox *dns;
    QLineEdit *device;
};
